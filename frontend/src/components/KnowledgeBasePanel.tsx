import { useState, useEffect, useCallback } from "react";
import {
  Upload,
  FileText,
  AlertCircle,
  Loader2,
  Plus,
  Trash2,
  FolderOpen,
  ChevronDown,
  ChevronRight,
  HardDrive,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { isMac } from "@/lib/platform";
import type { Session, SessionDocument, SessionGroup, IndexProgressPayload } from "@/lib/types";
import * as IndexService from "../../bindings/changeme/services/indexservice";
import { Events } from "@wailsio/runtime";

interface Props {
  sessions: Session[];
  activeSessionID: string | null;
  onSessionSelect: (id: string) => void;
  onSessionCreated: (session: Session) => void;
  onSessionDeleted: (id: string) => void;
  onSessionUpdated: (session: Session) => void;
}

interface DocProgress {
  docName: string;
  stage: string;
  pct: number;
}

export function KnowledgeBasePanel({
  sessions,
  activeSessionID,
  onSessionSelect,
  onSessionCreated,
  onSessionDeleted,
  onSessionUpdated,
}: Props) {
  const [newSessionName, setNewSessionName] = useState("");
  const [creatingSession, setCreatingSession] = useState(false);
  const [progress, setProgress] = useState<Record<string, DocProgress>>({});
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());

  const activeSession = sessions.find((s) => s.id === activeSessionID) ?? null;
  const isIndexing = Object.keys(progress).length > 0;

  // Listen to index events.
  useEffect(() => {
    const offProgress = Events.On("index:progress", (e: { data: IndexProgressPayload }) => {
      const { docID, docName, stage, pct } = e.data;
      setProgress((prev) => ({ ...prev, [docID]: { docName, stage, pct } }));
    });

    const offComplete = Events.On("index:complete", (e: { data: { sessionID: string } }) => {
      setProgress({});
      // Reload the updated session.
      IndexService.GetSession(e.data.sessionID).then((sess) => {
        if (sess) onSessionUpdated(sess as unknown as Session);
      });
    });

    const offError = Events.On("index:error", () => {
      setProgress({});
    });

    return () => {
      offProgress();
      offComplete();
      offError();
    };
  }, [onSessionUpdated]);

  const handleCreateSession = useCallback(async () => {
    const name = newSessionName.trim() || `Session ${sessions.length + 1}`;
    setCreatingSession(true);

    try {
      const sess = await IndexService.CreateSession(name);
      if (sess) {
        onSessionCreated(sess as unknown as Session);
        onSessionSelect(sess.id);
      }
    } finally {
      setNewSessionName("");
      setCreatingSession(false);
    }
  }, [newSessionName, sessions.length, onSessionCreated, onSessionSelect]);

  const handleDeleteSession = useCallback(
    async (id: string, e: React.MouseEvent) => {
      e.stopPropagation();
      await IndexService.DeleteSession(id);
      onSessionDeleted(id);
    },
    [onSessionDeleted]
  );

  const handlePickFiles = useCallback(async () => {
    if (!activeSessionID) return;
    const paths = await IndexService.PickFiles();
    if (paths && paths.length > 0) {
      await IndexService.IndexPaths(activeSessionID, paths);
    }
  }, [activeSessionID]);

  const handlePickFolder = useCallback(async () => {
    if (!activeSessionID) return;
    const path = await IndexService.PickFolder();
    if (path) {
      await IndexService.IndexPaths(activeSessionID, [path]);
    }
  }, [activeSessionID]);

  const handlePickDisk = useCallback(async () => {
    if (!activeSessionID) return;
    const path = await IndexService.PickDisk();
    if (path) {
      await IndexService.IndexPaths(activeSessionID, [path]);
    }
  }, [activeSessionID]);

  const toggleGroup = (id: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div
        className={cn("border-b px-3 py-3", isMac && "pt-10")}
        style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
      >
        <div
          className="flex items-center justify-between"
          style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
        >
          <h2 className="font-semibold text-sm">Knowledge Base</h2>
          <Button
            size="sm"
            variant="ghost"
            className="h-6 w-6 p-0"
            onClick={handleCreateSession}
            disabled={creatingSession}
            title="New session"
          >
            {creatingSession ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
          </Button>
        </div>
      </div>

      <ScrollArea className="flex-1">
        <div className="px-2 py-2 space-y-1">
          {/* Session list */}
          {sessions.length === 0 && (
            <p className="text-xs text-muted-foreground text-center py-4 px-2">
              No sessions yet. Click + to create one.
            </p>
          )}

          {sessions.map((sess) => (
            <div
              key={sess.id}
              onClick={() => onSessionSelect(sess.id)}
              className={cn(
                "group flex items-center justify-between rounded-md px-2 py-1.5 cursor-pointer text-sm transition-colors",
                sess.id === activeSessionID
                  ? "bg-primary/10 text-primary font-medium"
                  : "hover:bg-accent/50 text-muted-foreground hover:text-foreground"
              )}
            >
              <span className="truncate flex-1">{sess.name}</span>
              <button
                className="opacity-0 group-hover:opacity-100 ml-1 p-0.5 rounded hover:text-destructive transition-opacity"
                onClick={(e) => handleDeleteSession(sess.id, e)}
              >
                <Trash2 className="h-3 w-3" />
              </button>
            </div>
          ))}
        </div>

        {/* Active session content */}
        {activeSession && (
          <>
            <Separator className="mx-2" />

            {/* Ingest buttons */}
            <div className="px-3 py-2 space-y-1.5">
              <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                Add to &ldquo;{activeSession.name}&rdquo;
              </p>
              <div className="flex gap-1.5">
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1 gap-1.5 text-xs h-7"
                  onClick={handlePickFiles}
                  disabled={isIndexing}
                >
                  <Upload className="h-3 w-3" />
                  Files
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1 gap-1.5 text-xs h-7"
                  onClick={handlePickFolder}
                  disabled={isIndexing}
                >
                  <FolderOpen className="h-3 w-3" />
                  Folder
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1 gap-1.5 text-xs h-7"
                  onClick={handlePickDisk}
                  disabled={isIndexing}
                >
                  <HardDrive className="h-3 w-3" />
                  Disk
                </Button>
              </div>
            </div>

            {/* Progress overlay */}
            {isIndexing && (
              <div className="px-3 py-2 space-y-2">
                <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Indexing…
                </p>
                {Object.entries(progress).map(([docID, p]) => (
                  <div key={docID} className="space-y-0.5">
                    <div className="flex justify-between text-xs text-muted-foreground">
                      <span className="truncate flex-1">{p.docName}</span>
                      <span className="ml-2 shrink-0">{p.pct}%</span>
                    </div>
                    <div className="h-1 rounded-full bg-muted overflow-hidden">
                      <div
                        className="h-full bg-primary rounded-full transition-all duration-200"
                        style={{ width: `${p.pct}%` }}
                      />
                    </div>
                    <p className="text-xs text-muted-foreground/60">{p.stage}</p>
                  </div>
                ))}
              </div>
            )}

            {/* Groups + documents */}
            <div className="px-2 py-2">
              {activeSession.documents.length === 0 && !isIndexing && (
                <p className="text-xs text-muted-foreground text-center py-4 px-2">
                  No documents yet. Add files above.
                </p>
              )}

              {activeSession.groups.length > 0 ? (
                <GroupedView
                  session={activeSession}
                  expandedGroups={expandedGroups}
                  onToggleGroup={toggleGroup}
                />
              ) : (
                <FlatView docs={activeSession.documents} />
              )}
            </div>
          </>
        )}
      </ScrollArea>
    </div>
  );
}

function GroupedView({
  session,
  expandedGroups,
  onToggleGroup,
}: {
  session: Session;
  expandedGroups: Set<string>;
  onToggleGroup: (id: string) => void;
}) {
  const docMap = new Map(session.documents.map((d) => [d.id, d]));

  // Ungrouped docs
  const groupedIDs = new Set(session.groups.flatMap((g) => g.doc_ids));
  const ungrouped = session.documents.filter((d) => !groupedIDs.has(d.id));

  return (
    <div className="space-y-1">
      {session.groups.map((group) => (
        <GroupRow
          key={group.id}
          group={group}
          docs={group.doc_ids.map((id) => docMap.get(id)).filter(Boolean) as SessionDocument[]}
          expanded={expandedGroups.has(group.id)}
          onToggle={() => onToggleGroup(group.id)}
        />
      ))}

      {ungrouped.length > 0 && (
        <>
          {session.groups.length > 0 && <Separator className="my-1" />}
          {ungrouped.map((doc) => (
            <DocRow key={doc.id} doc={doc} />
          ))}
        </>
      )}
    </div>
  );
}

function FlatView({ docs }: { docs: SessionDocument[] }) {
  return (
    <div className="space-y-1">
      {docs.map((doc) => (
        <DocRow key={doc.id} doc={doc} />
      ))}
    </div>
  );
}

function GroupRow({
  group,
  docs,
  expanded,
  onToggle,
}: {
  group: SessionGroup;
  docs: SessionDocument[];
  expanded: boolean;
  onToggle: () => void;
}) {
  return (
    <div>
      <button
        onClick={onToggle}
        className="w-full flex items-center gap-1 px-1.5 py-1 rounded-md hover:bg-accent/50 text-xs font-semibold text-muted-foreground uppercase tracking-wider transition-colors"
      >
        {expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        <span className="truncate flex-1 text-left">{group.label}</span>
        <span className="text-xs font-normal normal-case">{docs.length}</span>
      </button>

      {expanded && (
        <div className="ml-3 space-y-0.5 mt-0.5">
          {docs.map((doc) => (
            <DocRow key={doc.id} doc={doc} compact />
          ))}
        </div>
      )}
    </div>
  );
}

function DocRow({ doc, compact = false }: { doc: SessionDocument; compact?: boolean }) {
  return (
    <div
      className={cn(
        "rounded-lg px-2 text-sm transition-colors hover:bg-accent/50 cursor-pointer",
        compact ? "py-1.5" : "p-2.5"
      )}
    >
      <div className="flex items-start gap-2">
        <FileText className="h-4 w-4 text-muted-foreground flex-shrink-0 mt-0.5" />
        <div className="flex-1 min-w-0">
          <p className="font-medium truncate text-xs leading-tight">{doc.name}</p>
          {!compact && (
            <div className="flex items-center gap-2 mt-1">
              <StatusBadge status={doc.status} />
              {doc.chunk_count > 0 && (
                <span className="text-xs text-muted-foreground">{doc.chunk_count} chunks</span>
              )}
            </div>
          )}
          {!compact && doc.summary && (
            <p className="text-xs text-muted-foreground mt-1 line-clamp-2">{doc.summary}</p>
          )}
        </div>
        {compact && <StatusDot status={doc.status} />}
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: SessionDocument["status"] }) {
  if (status === "indexed") return <Badge variant="success">Indexed</Badge>;
  if (status === "processing")
    return (
      <Badge variant="warning" className="gap-1">
        <Loader2 className="h-3 w-3 animate-spin" />
        Processing
      </Badge>
    );
  return (
    <Badge variant="error" className="gap-1">
      <AlertCircle className="h-3 w-3" />
      Error
    </Badge>
  );
}

function StatusDot({ status }: { status: SessionDocument["status"] }) {
  return (
    <div
      className={cn("h-2 w-2 rounded-full mt-1 flex-shrink-0", {
        "bg-green-500": status === "indexed",
        "bg-yellow-500 animate-pulse": status === "processing",
        "bg-red-500": status === "error",
      })}
    />
  );
}
