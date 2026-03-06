import { Upload, FileText, AlertCircle, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { isMac } from "@/lib/platform";
import type { KBDocument } from "@/lib/types";

interface KnowledgeBasePanelProps {
  documents: KBDocument[];
  activeSourceIds: string[];
}

function StatusBadge({ status }: { status: KBDocument["status"] }) {
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

export function KnowledgeBasePanel({ documents, activeSourceIds }: KnowledgeBasePanelProps) {
  const activeSources = documents.filter((d) => activeSourceIds.includes(d.id));
  const allDocs = documents.filter((d) => !activeSourceIds.includes(d.id));

  return (
    <div className="flex flex-col h-full">
      {/* pl-20 clears macOS traffic light buttons (~80px) */}
      <div
        className={cn("border-b px-3 pr-3 py-3 flex items-center justify-between", isMac && "pt-10")}
        style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
      >
        <div style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}>
          <h2 className="font-semibold text-sm">Knowledge Base</h2>
          <p className="text-xs text-muted-foreground">{documents.length} documents</p>
        </div>
        <div style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}>
          <Button size="sm" variant="outline" className="gap-1.5">
            <Upload className="h-3.5 w-3.5" />
            Upload
          </Button>
        </div>
      </div>

      <ScrollArea className="flex-1">
        <div className="px-3 py-3">
          {activeSources.length > 0 && (
            <>
              <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider px-1 mb-2">
                Sources used
              </p>
              <div className="space-y-1.5 mb-3">
                {activeSources.map((doc) => (
                  <DocRow key={doc.id} doc={doc} highlighted />
                ))}
              </div>
              <Separator className="my-3" />
              <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider px-1 mb-2">
                All Documents
              </p>
            </>
          )}
          <div className="space-y-1.5">
            {allDocs.map((doc) => (
              <DocRow key={doc.id} doc={doc} />
            ))}
          </div>
        </div>
      </ScrollArea>
    </div>
  );
}

function DocRow({ doc, highlighted = false }: { doc: KBDocument; highlighted?: boolean }) {
  return (
    <div
      className={`rounded-lg p-2.5 text-sm transition-colors hover:bg-accent/50 cursor-pointer ${
        highlighted ? "bg-primary/5 border border-primary/20" : ""
      }`}
    >
      <div className="flex items-start gap-2">
        <FileText className="h-4 w-4 text-muted-foreground flex-shrink-0 mt-0.5" />
        <div className="flex-1 min-w-0">
          <p className="font-medium truncate text-xs leading-tight">{doc.name}</p>
          <div className="flex items-center gap-2 mt-1">
            <StatusBadge status={doc.status} />
            {doc.chunks > 0 && (
              <span className="text-xs text-muted-foreground">{doc.chunks} chunks</span>
            )}
          </div>
          <p className="text-xs text-muted-foreground mt-0.5">{doc.size}</p>
        </div>
      </div>
    </div>
  );
}
