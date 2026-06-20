import { ImageOff, FolderInput } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import type { DesignInfo } from "@/lib/types";

interface Props {
  designs: DesignInfo[];
  hasAny: boolean;
  onSelect: (d: DesignInfo) => void;
}

export function GalleryGrid({ designs, hasAny, onSelect }: Props) {
  if (designs.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center p-8">
        <div className="flex flex-col items-center gap-3 text-center text-muted-foreground max-w-sm">
          {hasAny ? (
            <>
              <ImageOff className="h-10 w-10 opacity-40" />
              <p className="text-sm">Nenhum bordado corresponde aos filtros.</p>
            </>
          ) : (
            <>
              <FolderInput className="h-10 w-10 opacity-40" />
              <p className="text-sm">
                Importe uma pasta de bordados para começar. O StitchVault gera thumbnails e
                índices de busca automaticamente.
              </p>
            </>
          )}
        </div>
      </div>
    );
  }

  return (
    <ScrollArea className="flex-1">
      <div className="grid grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-3 p-4">
        {designs.map((d) => (
          <DesignCard key={d.id} design={d} onClick={() => onSelect(d)} />
        ))}
      </div>
    </ScrollArea>
  );
}

function DesignCard({ design, onClick }: { design: DesignInfo; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="group flex flex-col overflow-hidden rounded-lg border bg-card text-left transition-colors hover:border-primary/50 hover:bg-accent/30 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
    >
      <div className="aspect-square w-full overflow-hidden bg-[#FAFAF8]">
        <img
          src={design.thumbnailURL}
          alt={design.fileName}
          loading="lazy"
          className="h-full w-full object-contain transition-transform group-hover:scale-105"
        />
      </div>
      <div className="flex flex-col gap-1.5 p-2">
        <p className="truncate text-xs font-medium" title={design.fileName}>
          {design.fileName}
        </p>
        <div className="flex items-center justify-between gap-1">
          <Badge variant="secondary" className="font-mono text-[10px] uppercase">
            {design.format.replace(".", "")}
          </Badge>
          <span className="text-[10px] tabular-nums text-muted-foreground">
            {Math.round(design.widthMM)}×{Math.round(design.heightMM)} mm
          </span>
        </div>
        <div className="flex items-center justify-between gap-1">
          <PaletteDots palette={design.palette} />
          <span className="text-[10px] tabular-nums text-muted-foreground">
            {formatCount(design.stitchCount)} pts
          </span>
        </div>
      </div>
    </button>
  );
}

export function PaletteDots({ palette, max = 6 }: { palette: DesignInfo["palette"]; max?: number }) {
  if (!palette || palette.length === 0) {
    return <span className="text-[10px] text-muted-foreground/60">sem cor</span>;
  }
  const shown = palette.slice(0, max);
  const extra = palette.length - shown.length;
  return (
    <div className="flex items-center gap-0.5">
      {shown.map((c, i) => (
        <span
          key={i}
          className="h-3 w-3 rounded-full border border-black/10"
          style={{ backgroundColor: c.hex }}
          title={c.description || c.hex}
        />
      ))}
      {extra > 0 && <span className="text-[10px] text-muted-foreground">+{extra}</span>}
    </div>
  );
}

function formatCount(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k`;
  return String(n);
}
