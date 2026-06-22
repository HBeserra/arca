import { Check, FolderTree, Folder, Sparkles, Loader2, X, Cpu, MemoryStick, CopyCheck, Languages, Archive, Download, Upload } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Slider } from "@/components/ui/slider";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";
import type { FacetInfo, ListFilter, FolderInfo, VisionModelInfo, CaptionLanguageInfo } from "@/lib/types";
import { emptyFilter } from "@/lib/types";

interface Props {
  facets: FacetInfo | null;
  filter: ListFilter;
  onChange: (f: ListFilter) => void;
  folders: FolderInfo[];
  foldersAvailable: boolean;
  generatingFolders: boolean;
  onGenerateFolders: () => void;
  visionInfo: VisionModelInfo | null;
  onSetVisionModel: (id: string) => void;
  captionInfo: CaptionLanguageInfo | null;
  onSetCaptionLanguage: (code: string) => void;
  modelsLoaded: boolean;
  onEjectModels: () => void;
  aiBusy: boolean;
  onExportCatalog: () => void;
  onImportCatalog: () => void;
  catalogIO: { kind: "export" | "restore" | null; done: number; total: number };
}

export function FilterSidebar({
  facets,
  filter,
  onChange,
  folders,
  foldersAvailable,
  generatingFolders,
  onGenerateFolders,
  visionInfo,
  onSetVisionModel,
  captionInfo,
  onSetCaptionLanguage,
  modelsLoaded,
  onEjectModels,
  aiBusy,
  onExportCatalog,
  onImportCatalog,
  catalogIO,
}: Props) {
  const set = (patch: Partial<ListFilter>) => onChange({ ...filter, ...patch });

  const toggleFormat = (fmt: string) => {
    const has = filter.formats.includes(fmt);
    set({ formats: has ? filter.formats.filter((f) => f !== fmt) : [...filter.formats, fmt] });
  };

  const activeCount =
    filter.formats.length +
    (filter.minSizeMM > 0 || filter.maxSizeMM > 0 ? 1 : 0) +
    (filter.minStitches > 0 || filter.maxStitches > 0 ? 1 : 0) +
    (filter.minColors > 0 || filter.maxColors > 0 ? 1 : 0) +
    (filter.duplicatesOnly ? 1 : 0);

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-3 h-10 border-b flex-shrink-0">
        <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Filtros</span>
        {activeCount > 0 && (
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-2 text-xs"
            onClick={() => onChange({ ...emptyFilter })}
          >
            <X className="h-3 w-3" /> Limpar
          </Button>
        )}
      </div>

      <ScrollArea className="flex-1">
        <div className="p-3 space-y-5">
          {/* Duplicates toggle */}
          <button
            onClick={() => set({ duplicatesOnly: !filter.duplicatesOnly })}
            className={cn(
              "flex w-full items-center justify-between rounded-md px-2 py-1.5 text-sm transition-colors",
              filter.duplicatesOnly ? "bg-accent text-accent-foreground" : "hover:bg-muted"
            )}
            title="Mostrar apenas designs com cópias (mesma contagem de pontos, cores e dimensões)"
          >
            <span className="flex items-center gap-1.5">
              <CopyCheck className="h-3.5 w-3.5 opacity-70" /> Apenas duplicados
            </span>
            <span
              className={cn(
                "flex h-3.5 w-3.5 items-center justify-center rounded-sm border",
                filter.duplicatesOnly
                  ? "border-primary bg-primary text-primary-foreground"
                  : "border-muted-foreground/40"
              )}
            >
              {filter.duplicatesOnly && <Check className="h-3 w-3" />}
            </span>
          </button>

          {/* Formats */}
          <section className="space-y-1.5">
            <h3 className="text-xs font-medium text-muted-foreground">Formato</h3>
            {facets && facets.formats.length > 0 ? (
              facets.formats.map((f) => {
                const active = filter.formats.includes(f.value);
                return (
                  <button
                    key={f.value}
                    onClick={() => toggleFormat(f.value)}
                    className={cn(
                      "flex w-full items-center justify-between rounded-md px-2 py-1 text-sm transition-colors",
                      active ? "bg-accent text-accent-foreground" : "hover:bg-muted"
                    )}
                  >
                    <span className="flex items-center gap-1.5">
                      <span
                        className={cn(
                          "flex h-3.5 w-3.5 items-center justify-center rounded-sm border",
                          active ? "border-primary bg-primary text-primary-foreground" : "border-muted-foreground/40"
                        )}
                      >
                        {active && <Check className="h-3 w-3" />}
                      </span>
                      <span className="font-mono">{f.value}</span>
                    </span>
                    <span className="text-xs text-muted-foreground">{f.count}</span>
                  </button>
                );
              })
            ) : (
              <p className="text-xs text-muted-foreground/70">Sem dados ainda.</p>
            )}
          </section>

          <RangeRow
            label="Tamanho"
            unit="mm"
            facetMax={facets?.maxSizeMM ?? 0}
            min={filter.minSizeMM}
            max={filter.maxSizeMM}
            onChange={(lo, hi) => set({ minSizeMM: lo, maxSizeMM: hi })}
          />
          <RangeRow
            label="Pontos"
            facetMax={facets?.maxStitches ?? 0}
            min={filter.minStitches}
            max={filter.maxStitches}
            onChange={(lo, hi) => set({ minStitches: lo, maxStitches: hi })}
          />
          <RangeRow
            label="Cores"
            facetMax={facets?.maxColors ?? 0}
            min={filter.minColors}
            max={filter.maxColors}
            onChange={(lo, hi) => set({ minColors: lo, maxColors: hi })}
          />

          {/* Virtual folders — LLM-generated */}
          <section className="space-y-1.5 pt-2 border-t">
            <div className="flex items-center justify-between">
              <h3 className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                <FolderTree className="h-3.5 w-3.5" /> Pastas virtuais
              </h3>
              {foldersAvailable && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-1.5 text-xs"
                  onClick={onGenerateFolders}
                  disabled={generatingFolders}
                  title="Gerar pastas com IA a partir do conteúdo dos bordados"
                >
                  {generatingFolders ? <Loader2 className="h-3 w-3 animate-spin" /> : <Sparkles className="h-3 w-3" />}
                  {folders.length > 0 ? "Regenerar" : "Gerar"}
                </Button>
              )}
            </div>

            {!foldersAvailable ? (
              <p className="text-xs text-muted-foreground/70 leading-relaxed">
                Classifique os bordados (botão "Classificar IA") para organizá-los por tema.
              </p>
            ) : folders.length === 0 ? (
              <p className="text-xs text-muted-foreground/70 leading-relaxed">
                {generatingFolders ? "Gerando pastas…" : 'Nenhuma pasta ainda — clique em "Gerar".'}
              </p>
            ) : (
              <div className="space-y-0.5">
                <button
                  onClick={() => set({ virtualFolderID: "" })}
                  className={cn(
                    "flex w-full items-center rounded-md px-2 py-1 text-sm transition-colors",
                    filter.virtualFolderID === "" ? "bg-accent text-accent-foreground" : "hover:bg-muted"
                  )}
                >
                  Todas
                </button>
                {folders.map((f) => {
                  const active = filter.virtualFolderID === f.id;
                  return (
                    <button
                      key={f.id}
                      onClick={() => set({ virtualFolderID: active ? "" : f.id })}
                      className={cn(
                        "flex w-full items-center justify-between gap-1 rounded-md px-2 py-1 text-sm transition-colors",
                        active ? "bg-accent text-accent-foreground" : "hover:bg-muted"
                      )}
                    >
                      <span className="flex items-center gap-1.5 truncate">
                        <Folder className="h-3.5 w-3.5 flex-shrink-0 opacity-70" />
                        <span className="truncate">{f.name}</span>
                      </span>
                      <span className="text-xs text-muted-foreground">{f.count}</span>
                    </button>
                  );
                })}
              </div>
            )}
          </section>

          {/* AI vision model picker + eject */}
          <section className="space-y-1.5 pt-2 border-t">
            <h3 className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
              <Cpu className="h-3.5 w-3.5" /> Modelo de IA
            </h3>
            {visionInfo && visionInfo.models.length > 0 ? (
              <>
                <Select value={visionInfo.currentID || undefined} onValueChange={onSetVisionModel} disabled={aiBusy}>
                  <SelectTrigger className="h-8 text-sm">
                    <SelectValue placeholder="Selecionar modelo" />
                  </SelectTrigger>
                  <SelectContent>
                    {visionInfo.models.map((m) => (
                      <SelectItem key={m.id} value={m.id} className="text-sm">
                        {m.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-[11px] leading-relaxed text-muted-foreground/70">
                  {visionInfo.models.find((m) => m.id === visionInfo.currentID)?.description ??
                    "Troca recarrega o modelo na próxima classificação."}
                </p>
              </>
            ) : (
              <p className="text-xs text-muted-foreground/70">Modelo de visão indisponível.</p>
            )}

            {captionInfo && captionInfo.options.length > 0 && (
              <div className="space-y-1 pt-1">
                <label className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                  <Languages className="h-3 w-3" /> Idioma da descrição
                </label>
                <Select value={captionInfo.current || undefined} onValueChange={onSetCaptionLanguage} disabled={aiBusy}>
                  <SelectTrigger className="h-8 text-sm">
                    <SelectValue placeholder="Idioma" />
                  </SelectTrigger>
                  <SelectContent>
                    {captionInfo.options.map((o) => (
                      <SelectItem key={o.code} value={o.code} className="text-sm">
                        {o.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            {modelsLoaded && (
              <Button
                variant="ghost"
                size="sm"
                className="h-6 px-2 text-xs"
                onClick={onEjectModels}
                disabled={aiBusy}
                title="Descarregar o modelo da memória (recarrega no próximo uso)"
              >
                <MemoryStick className="h-3 w-3" /> Liberar memória
              </Button>
            )}
          </section>

          {/* Catalog export / import (.svault portable bundle) */}
          <section className="space-y-1.5 pt-2 border-t">
            <h3 className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
              <Archive className="h-3.5 w-3.5" /> Catálogo
            </h3>
            {catalogIO.kind ? (
              <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Loader2 className="h-3 w-3 animate-spin" />
                {catalogIO.kind === "export" ? "Exportando" : "Importando"}…{" "}
                {catalogIO.total > 0 ? `${catalogIO.done}/${catalogIO.total}` : ""}
              </p>
            ) : (
              <div className="flex flex-col gap-0.5">
                <Button variant="ghost" size="sm" className="h-7 justify-start px-2 text-xs" onClick={onExportCatalog}>
                  <Download className="h-3.5 w-3.5" /> Exportar catálogo
                </Button>
                <Button variant="ghost" size="sm" className="h-7 justify-start px-2 text-xs" onClick={onImportCatalog}>
                  <Upload className="h-3.5 w-3.5" /> Importar catálogo
                </Button>
              </div>
            )}
            <p className="text-[11px] leading-relaxed text-muted-foreground/70">
              Pacote portátil (.svault) com índice, miniaturas e arquivos — abra em outro computador sem reprocessar.
            </p>
          </section>
        </div>
      </ScrollArea>
    </div>
  );
}

interface RangeRowProps {
  label: string;
  unit?: string;
  facetMax: number;
  min: number;
  max: number;
  onChange: (lo: number, hi: number) => void;
}

function RangeRow({ label, unit, facetMax, min, max, onChange }: RangeRowProps) {
  const ceiling = Math.max(1, Math.ceil(facetMax));
  const lo = min;
  const hi = max > 0 ? max : ceiling;
  const disabled = facetMax <= 0;

  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between">
        <h3 className="text-xs font-medium text-muted-foreground">{label}</h3>
        <span className="text-xs tabular-nums text-muted-foreground">
          {lo}–{hi}
          {unit ? ` ${unit}` : ""}
        </span>
      </div>
      <Slider
        value={[lo, hi]}
        min={0}
        max={ceiling}
        step={1}
        disabled={disabled}
        onValueChange={(v: number[]) => {
          const [a, b] = v;
          // Store 0 when the thumb sits at an extreme so the filter is "unset".
          onChange(a <= 0 ? 0 : a, b >= ceiling ? 0 : b);
        }}
      />
    </section>
  );
}
