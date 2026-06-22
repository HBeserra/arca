import { useRef, useState, useEffect, useLayoutEffect } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ImageOff, FolderInput, Loader2, Check } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { DesignInfo } from "@/lib/types";

interface Props {
  designs: DesignInfo[];
  hasAny: boolean;
  onSelect: (d: DesignInfo) => void;
  onLoadMore: () => void;
  hasMore: boolean;
  loadingMore: boolean;
  /** Bumped by the parent to scroll back to the top (e.g. on filter change). */
  resetToken: number;
  selectedIds: Set<string>;
  onToggleSelect: (id: string) => void;
}

// Layout constants — mirror the CSS so windowing math matches what renders.
const GAP = 12; // gap between cards (px)
const PAD = 16; // padding around the grid (px)
const MIN_COL = 180; // minimum card width (px) — matches the old auto-fill grid

// GalleryGrid renders the catalog as a virtualized grid: only the rows currently
// in (or near) the viewport exist in the DOM, so 10k+ designs stay smooth. Scrolling
// near the end calls onLoadMore for infinite paging.
export function GalleryGrid({ designs, hasAny, onSelect, onLoadMore, hasMore, loadingMore, resetToken, selectedIds, onToggleSelect }: Props) {
  const selectionActive = selectedIds.size > 0;
  const parentRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);

  // Track the scroll container width to compute the responsive column count.
  useLayoutEffect(() => {
    const el = parentRef.current;
    if (!el) return;
    setWidth(el.clientWidth);
    const ro = new ResizeObserver((entries) => setWidth(entries[0].contentRect.width));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const avail = Math.max(0, width - 2 * PAD);
  const cols = Math.max(1, Math.floor((avail + GAP) / (MIN_COL + GAP)));
  const colW = (avail - (cols - 1) * GAP) / cols;
  const estRow = (colW > 0 ? colW : MIN_COL) + 78 + GAP; // square image + meta block + gap
  const rowCount = Math.ceil(designs.length / cols);

  const virt = useVirtualizer({
    count: rowCount,
    getScrollElement: () => parentRef.current,
    estimateSize: () => estRow,
    overscan: 3,
  });

  // Re-measure when the column count or estimated row height changes (resize).
  useEffect(() => {
    virt.measure();
  }, [cols, estRow, virt]);

  // Scroll back to top when the parent signals a fresh result set.
  useEffect(() => {
    parentRef.current?.scrollTo({ top: 0 });
  }, [resetToken]);

  // Infinite scroll: load the next page once the last rendered row nears the end.
  const items = virt.getVirtualItems();
  useEffect(() => {
    const last = items[items.length - 1];
    if (last && hasMore && !loadingMore && last.index >= rowCount - 2) {
      onLoadMore();
    }
  }, [items, hasMore, loadingMore, rowCount, onLoadMore]);

  // The scroll container is ALWAYS mounted (even when empty) so parentRef is set
  // when the width-measuring effect runs; otherwise cols would stay 1 and each
  // card would stretch to the full viewport width.
  return (
    <div ref={parentRef} className="flex-1 overflow-auto">
      {designs.length === 0 ? (
        <div className="flex h-full items-center justify-center p-8">
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
      ) : (
        <>
          <div style={{ height: virt.getTotalSize(), position: "relative", width: "100%" }}>
            {items.map((vRow) => {
              const start = vRow.index * cols;
              const rowItems = designs.slice(start, start + cols);
              return (
                <div
                  key={vRow.key}
                  data-index={vRow.index}
                  ref={virt.measureElement}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    transform: `translateY(${vRow.start}px)`,
                    display: "grid",
                    gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
                    gap: GAP,
                    paddingLeft: PAD,
                    paddingRight: PAD,
                    paddingTop: vRow.index === 0 ? PAD : 0,
                    paddingBottom: GAP,
                  }}
                >
                  {rowItems.map((d) => (
                    <DesignCard
                      key={d.id}
                      design={d}
                      onClick={() => onSelect(d)}
                      selected={selectedIds.has(d.id)}
                      selectionActive={selectionActive}
                      onToggleSelect={() => onToggleSelect(d.id)}
                    />
                  ))}
                </div>
              );
            })}
          </div>
          {loadingMore && (
            <div className="flex items-center justify-center gap-2 py-4 text-xs text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" /> Carregando mais…
            </div>
          )}
        </>
      )}
    </div>
  );
}

function DesignCard({
  design,
  onClick,
  selected,
  selectionActive,
  onToggleSelect,
}: {
  design: DesignInfo;
  onClick: () => void;
  selected: boolean;
  selectionActive: boolean;
  onToggleSelect: () => void;
}) {
  return (
    <div
      className={cn(
        "group relative flex flex-col overflow-hidden rounded-lg border bg-card transition-colors",
        selected ? "border-primary ring-1 ring-primary" : "hover:border-primary/50 hover:bg-accent/30"
      )}
    >
      <button
        type="button"
        aria-label={selected ? "Desmarcar" : "Selecionar"}
        onClick={(e) => {
          e.stopPropagation();
          onToggleSelect();
        }}
        className={cn(
          "absolute left-1.5 top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded border bg-background/90 shadow-sm transition-opacity",
          selected
            ? "border-primary bg-primary text-primary-foreground opacity-100"
            : cn("border-muted-foreground/40", selectionActive ? "opacity-100" : "opacity-0 group-hover:opacity-100")
        )}
      >
        {selected && <Check className="h-3.5 w-3.5" />}
      </button>

      <button onClick={onClick} className="flex flex-col text-left focus-visible:outline-none">
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
    </div>
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
