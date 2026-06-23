import { useState, useEffect } from "react";
import type { ReactNode } from "react";
import { Sparkles, Loader2, Trash2, ExternalLink, FolderOpen } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { DesignInfo } from "@/lib/types";
import * as CatalogService from "../../bindings/stitchvault/services/catalogservice";

interface Props {
  design: DesignInfo | null;
  classifyAvailable: boolean;
  onClose: () => void;
  onClassified: (d: DesignInfo) => void;
  onDeleted: (id: string) => void;
}

export function DesignDetailDialog({ design, classifyAvailable, onClose, onClassified, onDeleted }: Props) {
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [fileAction, setFileAction] = useState<null | "open" | "reveal">(null);
  const [fileError, setFileError] = useState("");

  // Reset transient state whenever a different design is opened.
  useEffect(() => {
    setConfirmDelete(false);
    setFileError("");
  }, [design?.id]);

  const classify = async () => {
    if (!design) return;
    setBusy(true);
    try {
      const updated = await CatalogService.ClassifyDesign(design.id);
      if (updated) onClassified(updated as unknown as DesignInfo);
    } finally {
      setBusy(false);
    }
  };

  const doDelete = async () => {
    if (!design) return;
    setDeleting(true);
    try {
      await CatalogService.DeleteDesign(design.id);
      onDeleted(design.id);
    } finally {
      setDeleting(false);
      setConfirmDelete(false);
    }
  };

  const openFile = async () => {
    if (!design) return;
    setFileAction("open");
    setFileError("");
    try {
      await CatalogService.OpenDesignFile(design.id);
    } catch {
      setFileError("Não foi possível abrir o arquivo — ele pode ter sido movido ou removido.");
    } finally {
      setFileAction(null);
    }
  };

  const revealFile = async () => {
    if (!design) return;
    setFileAction("reveal");
    setFileError("");
    try {
      await CatalogService.RevealDesignFile(design.id);
    } catch {
      setFileError("Não foi possível localizar o arquivo na pasta.");
    } finally {
      setFileAction(null);
    }
  };

  return (
    <Dialog open={design !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-3xl">
        {design && (
          <>
            <DialogHeader>
              <DialogTitle className="truncate pr-8">{design.fileName}</DialogTitle>
              <DialogDescription className="truncate font-mono text-xs">{design.path}</DialogDescription>
            </DialogHeader>

            <div className="grid grid-cols-[minmax(0,1fr)_220px] gap-5">
              <div className="overflow-hidden rounded-md border bg-[#FAFAF8]">
                <img src={design.thumbnailURL} alt={design.fileName} className="h-full w-full object-contain" />
              </div>

              <ScrollArea className="max-h-[60vh]">
                <div className="space-y-4 pr-3">
                  <dl className="space-y-2 text-sm">
                    <Row label="Formato" value={<Badge variant="secondary" className="font-mono uppercase">{design.format.replace(".", "")}</Badge>} />
                    <Row label="Tamanho" value={`${design.widthMM.toFixed(1)} × ${design.heightMM.toFixed(1)} mm`} />
                    <Row label="Pontos" value={design.stitchCount.toLocaleString()} />
                    <Row label="Trocas de cor" value={String(design.colorChanges)} />
                    <Row label="Cores" value={String(design.colorCount)} />
                    <Row label="Arquivo" value={formatBytes(design.fileSize)} />
                  </dl>

                  <div className="space-y-1.5">
                    <h4 className="text-xs font-medium text-muted-foreground">Paleta</h4>
                    {design.palette.length === 0 ? (
                      <p className="text-xs text-muted-foreground/70">Este formato não armazena cores.</p>
                    ) : (
                      <ul className="space-y-1">
                        {design.palette.map((c, i) => (
                          <li key={i} className="flex items-center gap-2 text-xs">
                            <span className="h-4 w-4 flex-shrink-0 rounded border border-black/10" style={{ backgroundColor: c.hex }} />
                            <span className="font-mono text-muted-foreground">{c.hex}</span>
                            {c.description && <span className="truncate">{c.description}</span>}
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>

                  <div className="space-y-2 border-t pt-3">
                    <h4 className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                      <Sparkles className="h-3.5 w-3.5" /> Classificação por IA
                    </h4>
                    {design.caption ? (
                      <div className="space-y-1.5">
                        <p className="text-xs">{design.caption}</p>
                        {design.style && (
                          <p className="text-xs text-muted-foreground">Estilo: {design.style}</p>
                        )}
                        {design.tags.length > 0 && (
                          <div className="flex flex-wrap gap-1">
                            {design.tags.map((t) => (
                              <Badge key={t} variant="outline" className="text-[10px]">{t}</Badge>
                            ))}
                          </div>
                        )}
                        {classifyAvailable && (
                          <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={classify} disabled={busy}>
                            {busy ? <Loader2 className="h-3 w-3 animate-spin" /> : <Sparkles className="h-3 w-3" />}
                            Reclassificar
                          </Button>
                        )}
                      </div>
                    ) : classifyAvailable ? (
                      <Button variant="outline" size="sm" className="h-7 text-xs" onClick={classify} disabled={busy}>
                        {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Sparkles className="h-3.5 w-3.5" />}
                        {busy ? "Classificando…" : "Classificar com IA"}
                      </Button>
                    ) : (
                      <p className="text-xs text-muted-foreground/70">Modelo de visão indisponível.</p>
                    )}
                  </div>
                </div>
              </ScrollArea>
            </div>

            <div className="space-y-2 border-t pt-3">
              {fileError && <p className="text-xs text-destructive">{fileError}</p>}
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" className="h-7 text-xs" onClick={openFile} disabled={fileAction !== null}>
                  {fileAction === "open" ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ExternalLink className="h-3.5 w-3.5" />}
                  Abrir arquivo
                </Button>
                <Button variant="outline" size="sm" className="h-7 text-xs" onClick={revealFile} disabled={fileAction !== null}>
                  {fileAction === "reveal" ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FolderOpen className="h-3.5 w-3.5" />}
                  Mostrar na pasta
                </Button>

                {confirmDelete ? (
                  <>
                    <span className="ml-auto text-xs text-muted-foreground">Remover este bordado do vault?</span>
                    <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => setConfirmDelete(false)} disabled={deleting}>
                      Cancelar
                    </Button>
                    <Button variant="destructive" size="sm" className="h-7 text-xs" onClick={doDelete} disabled={deleting}>
                      {deleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                      Remover
                    </Button>
                  </>
                ) : (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="ml-auto h-7 text-xs text-destructive hover:text-destructive"
                    onClick={() => setConfirmDelete(true)}
                  >
                    <Trash2 className="h-3.5 w-3.5" /> Remover do vault
                  </Button>
                )}
              </div>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

function Row({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{value}</dd>
    </div>
  );
}

function formatBytes(n: number): string {
  if (n <= 0) return "—";
  const units = ["B", "KB", "MB", "GB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}
