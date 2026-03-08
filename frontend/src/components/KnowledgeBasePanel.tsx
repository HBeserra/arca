import { useState, useEffect, useCallback, useRef } from "react";
import {
  Upload,
  FileText,
  Plus,
  Trash2,
  Pencil,
  Check,
  X,
  FolderOpen,
  HardDrive,
  Folder,
  ChevronRight,
  Loader2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import type { Session, SessionDocument, IndexProgressPayload } from "@/lib/types";
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
  const [isIndexing, setIsIndexing] = useState(false);
  const [progress, setProgress] = useState<Record<string, DocProgress>>({});
  const [renamingSession, setRenamingSession] = useState(false);
  const renameDraftRef = useRef("");           // always current, no stale-closure risk
  const renameInputRef = useRef<HTMLInputElement>(null);

  const activeSession = sessions.find((s) => s.id === activeSessionID) ?? null;

  // Use a ref so event listeners always see the latest callback without
  // needing to re-subscribe every time onSessionUpdated changes.
  const onSessionUpdatedRef = useRef(onSessionUpdated);
  useEffect(() => { onSessionUpdatedRef.current = onSessionUpdated; }, [onSessionUpdated]);

  // Listen to index events — register once, never re-subscribe.
  useEffect(() => {
    const offProgress = Events.On("index:progress", (e: { data: IndexProgressPayload }) => {
      const { docID, docName, stage, pct } = e.data;
      setProgress((prev) => ({ ...prev, [docID]: { docName, stage, pct } }));
    });

    const offComplete = Events.On("index:complete", (e: { data: { sessionID: string } }) => {
      setIsIndexing(false);
      setProgress({});
      IndexService.GetSession(e.data.sessionID).then((sess) => {
        if (sess) onSessionUpdatedRef.current(sess as unknown as Session);
      });
    });

    const offError = Events.On("index:error", () => {
      setIsIndexing(false);
      setProgress({});
    });

    return () => {
      offProgress();
      offComplete();
      offError();
    };
  }, []); // empty — listeners are stable, callback accessed via ref

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

  const activeSessionIDRef = useRef(activeSessionID);
  useEffect(() => { activeSessionIDRef.current = activeSessionID; }, [activeSessionID]);
  const onSessionUpdatedRef2 = useRef(onSessionUpdated);
  useEffect(() => { onSessionUpdatedRef2.current = onSessionUpdated; }, [onSessionUpdated]);

  const handleStartRename = useCallback(() => {
    if (!activeSession) return;
    renameDraftRef.current = activeSession.name;
    if (renameInputRef.current) renameInputRef.current.value = activeSession.name;
    setRenamingSession(true);
    setTimeout(() => {
      renameInputRef.current?.focus();
      renameInputRef.current?.select();
    }, 0);
  }, [activeSession]);

  // Uses refs so onBlur never captures a stale value
  const handleCommitRename = useCallback(async () => {
    const id = activeSessionIDRef.current;
    if (!id) return;
    const trimmed = renameDraftRef.current.trim();
    setRenamingSession(false);
    if (!trimmed) return;
    await IndexService.RenameSession(id, trimmed);
    const sess = await IndexService.GetSession(id);
    if (sess) onSessionUpdatedRef2.current(sess as unknown as Session);
  }, []);

  const handlePickFiles = useCallback(async () => {
    if (!activeSessionID) return;
    const paths = await IndexService.PickFiles();
    if (paths && paths.length > 0) {
      setIsIndexing(true);
      await IndexService.IndexPaths(activeSessionID, paths);
    }
  }, [activeSessionID]);

  const handlePickFolder = useCallback(async () => {
    if (!activeSessionID) return;
    const path = await IndexService.PickFolder();
    if (path) {
      setIsIndexing(true);
      await IndexService.IndexPaths(activeSessionID, [path]);
    }
  }, [activeSessionID]);

  const handlePickDisk = useCallback(async () => {
    if (!activeSessionID) return;
    const path = await IndexService.PickDisk();
    if (path) {
      setIsIndexing(true);
      await IndexService.IndexPaths(activeSessionID, [path]);
    }
  }, [activeSessionID]);

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div
        className="border-b px-3 py-3 space-y-2"
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

        {/* Session selector */}
        <div
          className="flex items-center gap-1"
          style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
        >
          {renamingSession && activeSession ? (
            <>
              <input
                ref={renameInputRef}
                defaultValue={activeSession?.name}
                onChange={(e) => { renameDraftRef.current = e.target.value; }}
                onBlur={handleCommitRename}
                onKeyDown={(e) => {
                  if (e.key === "Enter") { e.preventDefault(); handleCommitRename(); }
                  if (e.key === "Escape") setRenamingSession(false);
                }}
                className="flex-1 min-w-0 h-7 px-2 text-xs rounded-md border border-input bg-background outline-none focus:ring-1 focus:ring-ring"
              />
              <Button size="sm" variant="ghost" className="h-7 w-7 p-0 flex-shrink-0 text-primary" onMouseDown={(e) => { e.preventDefault(); handleCommitRename(); }} title="Save">
                <Check className="h-3 w-3" />
              </Button>
              <Button size="sm" variant="ghost" className="h-7 w-7 p-0 flex-shrink-0" onMouseDown={(e) => { e.preventDefault(); setRenamingSession(false); }} title="Cancel">
                <X className="h-3 w-3" />
              </Button>
            </>
          ) : (
            <>
              {sessions.length === 0 ? (
                <p className="text-xs text-muted-foreground flex-1">No sessions yet.</p>
              ) : (
                <Select value={activeSessionID ?? ""} onValueChange={onSessionSelect}>
                  <SelectTrigger className="h-7 text-xs flex-1 min-w-0">
                    <SelectValue placeholder="Select session…" />
                  </SelectTrigger>
                  <SelectContent>
                    {sessions.map((sess) => (
                      <SelectItem key={sess.id} value={sess.id} className="text-xs">
                        {sess.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
              {activeSessionID && (
                <>
                  <Button size="sm" variant="ghost" className="h-7 w-7 p-0 flex-shrink-0" title="Rename session" onClick={handleStartRename}>
                    <Pencil className="h-3 w-3" />
                  </Button>
                  <Button size="sm" variant="ghost" className="h-7 w-7 p-0 flex-shrink-0 hover:text-destructive" title="Delete session" onClick={(e) => handleDeleteSession(activeSessionID, e)}>
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </>
              )}
            </>
          )}
        </div>
      </div>

      <ScrollArea className="flex-1">
        {/* Active session content */}
        {activeSession && (
          <>
            {/* Ingest buttons */}
            <div className="px-3 py-2 space-y-1.5">
              <div className="flex gap-1.5">
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1 gap-1.5 text-xs h-7"
                  onClick={handlePickFiles}
                  disabled={isIndexing}
                >
                  <Upload className="h-3 w-3" />
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1 gap-1.5 text-xs h-7"
                  onClick={handlePickFolder}
                  disabled={isIndexing}
                >
                  <FolderOpen className="h-3 w-3" />
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1 gap-1.5 text-xs h-7"
                  onClick={handlePickDisk}
                  disabled={isIndexing}
                >
                  <HardDrive className="h-3 w-3" />
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

            {/* Documents */}
            <div className="px-2 py-2">
              {activeSession.documents.length === 0 && !isIndexing && (
                <p className="text-xs text-muted-foreground text-center py-4 px-2">
                  No documents yet. Add files above.
                </p>
              )}
              <DocTree docs={activeSession.documents} />
            </div>
          </>
        )}
      </ScrollArea>
    </div>
  );
}


// ─── Tree view ─────────────────────────────────────────────────────────────

interface TreeNode {
  doc: SessionDocument;
  children: TreeNode[];
}

function buildTree(docs: SessionDocument[]): TreeNode[] {
  const nodes = new Map<string, TreeNode>(docs.map((d) => [d.id, { doc: d, children: [] }]));
  const roots: TreeNode[] = [];
  
  for (const doc of docs) {
    const node = nodes.get(doc.id)!;
    if (doc.parent_id && nodes.has(doc.parent_id)) {
      nodes.get(doc.parent_id)!.children.push(node);
    } else {
      roots.push(node);
    }
  }

  // sort by type then name
  const sortFn = (a: TreeNode, b: TreeNode) => {
    if (a.doc.type === b.doc.type) {
      return a.doc.name.localeCompare(b.doc.name);
    }
    return a.doc.type === "folder" ? -1 : 1;
  };
  
  for (const node of nodes.values()) {
    node.children.sort(sortFn);
  }

  return roots
}

function DocTree({ docs }: { docs: SessionDocument[] }) {
  const roots = buildTree(docs);
  return (
    <div className="space-y-0.5">
      {roots.map((node) => (
        <TreeNodeRow key={node.doc.id} node={node} />
      ))}
    </div>
  );
}

function TreeNodeRow({ node }: { node: TreeNode }) {
  if (node.doc.type === "folder") return <FolderRow node={node} />;
  return <FileRow doc={node.doc} />;
}

function countFiles(node: TreeNode): number {
  if (node.doc.type === "file") return 1;
  return node.children.reduce((sum, child) => sum + countFiles(child), 0);
}

type FolderStatus = "processing" | "error" | "idle";

function folderStatus(node: TreeNode): FolderStatus {
  if (node.doc.type === "file") {
    if (node.doc.status === "processing") return "processing";
    if (node.doc.status === "error") return "error";
    return "idle";
  }
  let hasError = false;
  for (const child of node.children) {
    const s = folderStatus(child);
    if (s === "processing") return "processing";
    if (s === "error") hasError = true;
  }
  return hasError ? "error" : "idle";
}

function FolderRow({ node }: { node: TreeNode }) {
  const [open, setOpen] = useState(false);
  const fileCount = countFiles(node);
  const status = folderStatus(node);

  return (
    <div>
      <button
        onClick={() => setOpen((o) => !o)}
        className="w-full flex items-center gap-1.5 rounded-md px-2 py-1.5 text-xs font-medium hover:bg-accent/50 transition-colors"
      >
        <ChevronRight
          className={cn("h-3 w-3 text-muted-foreground flex-shrink-0 transition-transform", open && "rotate-90")}
        />
        <Folder className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />
        <span className="truncate flex-1 text-left">{node.doc.name}</span>
        <span className="text-muted-foreground/60 shrink-0">{fileCount}</span>
        {status === "processing" && (
          <div className="h-2 w-2 rounded-full bg-yellow-500 animate-pulse flex-shrink-0" />
        )}
        {status === "error" && (
          <div className="h-2 w-2 rounded-full bg-red-500 flex-shrink-0" />
        )}
      </button>

      {open && node.children.length > 0 && (
        <div className="ml-3 border-l border-border/50 pl-1.5 space-y-0.5 mt-0.5 mb-1">
          {node.children.map((child) => (
            <TreeNodeRow key={child.doc.id} node={child} />
          ))}
        </div>
      )}
    </div>
  );
}

function FileRow({ doc }: { doc: SessionDocument }) {
  return (
    <div className="flex items-center gap-2 rounded-md px-2 py-1.5 text-xs hover:bg-accent/50 transition-colors cursor-default">
      <FileText className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />
      <span className="truncate flex-1">{doc.name}</span>
      <StatusDot status={doc.status} />
    </div>
  );
}

function StatusDot({ status }: { status: SessionDocument["status"] }) {
  return (
    <div
      className={cn("h-2 w-2 rounded-full flex-shrink-0", {
        "bg-green-500": status === "completed",
        "bg-yellow-500 animate-pulse": status === "processing",
        "bg-red-500": status === "error",
      })}
    />
  );
}
