import { useState, useEffect, useCallback, useRef } from "react";
import { Events } from "@wailsio/runtime";
import { X } from "lucide-react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toolbar } from "@/components/Toolbar";
import { FilterSidebar } from "@/components/FilterSidebar";
import { GalleryGrid } from "@/components/GalleryGrid";
import { DesignDetailDialog } from "@/components/DesignDetailDialog";
import { emptyFilter } from "@/lib/types";
import type {
  DesignInfo,
  FacetInfo,
  ListFilter,
  ImportProgressPayload,
  ImportCompletePayload,
  ImportErrorPayload,
} from "@/lib/types";
import * as CatalogService from "../bindings/stitchvault/services/catalogservice";

interface ImportState {
  active: boolean;
  done: number;
  total: number;
}

export default function App() {
  const [designs, setDesigns] = useState<DesignInfo[]>([]);
  const [facets, setFacets] = useState<FacetInfo | null>(null);
  const [count, setCount] = useState(0);
  const [filter, setFilter] = useState<ListFilter>(emptyFilter);
  const [selected, setSelected] = useState<DesignInfo | null>(null);
  const [imp, setImp] = useState<ImportState>({ active: false, done: 0, total: 0 });
  const [errors, setErrors] = useState<string[]>([]);

  const refreshDesigns = useCallback((f: ListFilter) => {
    CatalogService.ListDesigns(f as never).then((d) =>
      setDesigns((d ?? []) as unknown as DesignInfo[])
    );
    CatalogService.CountDesigns(f as never).then((n) => setCount(n ?? 0));
  }, []);

  const refreshFacets = useCallback(() => {
    CatalogService.Facets().then((f) => {
      if (f) setFacets(f as unknown as FacetInfo);
    });
  }, []);

  // Facets once on mount (refreshed after each import).
  useEffect(() => {
    refreshFacets();
  }, [refreshFacets]);

  // Designs whenever the filter changes (and on mount).
  useEffect(() => {
    refreshDesigns(filter);
  }, [filter, refreshDesigns]);

  // Keep latest refs for the one-time event subscription.
  const refreshDesignsRef = useRef(refreshDesigns);
  const refreshFacetsRef = useRef(refreshFacets);
  const filterRef = useRef(filter);
  useEffect(() => {
    refreshDesignsRef.current = refreshDesigns;
    refreshFacetsRef.current = refreshFacets;
    filterRef.current = filter;
  });

  useEffect(() => {
    const offProgress = Events.On("import:progress", (e: { data: ImportProgressPayload }) => {
      setImp({ active: true, done: e.data.done, total: e.data.total });
    });
    const offError = Events.On("import:error", (e: { data: ImportErrorPayload }) => {
      setErrors((prev) => [...prev.slice(-19), `${e.data.fileName}: ${e.data.error}`]);
    });
    const offComplete = Events.On("import:complete", (e: { data: ImportCompletePayload }) => {
      setImp({ active: false, done: e.data.total, total: e.data.total });
      refreshDesignsRef.current(filterRef.current);
      refreshFacetsRef.current();
    });
    return () => {
      offProgress();
      offError();
      offComplete();
    };
  }, []);

  const startImport = useCallback((paths: string[]) => {
    if (!paths || paths.length === 0) return;
    setErrors([]);
    setImp({ active: true, done: 0, total: 0 });
    CatalogService.ImportPaths(paths as never);
  }, []);

  const handleImportFolder = useCallback(async () => {
    const path = await CatalogService.PickFolder();
    if (path) startImport([path]);
  }, [startImport]);

  const handleImportFiles = useCallback(async () => {
    const paths = await CatalogService.PickFiles();
    if (paths) startImport(paths as unknown as string[]);
  }, [startImport]);

  return (
    <TooltipProvider delayDuration={300}>
      <div className="flex flex-col h-screen w-screen overflow-hidden bg-background text-foreground">
        <Toolbar
          search={filter.search}
          onSearch={(s) => setFilter((f) => ({ ...f, search: s }))}
          onImportFolder={handleImportFolder}
          onImportFiles={handleImportFiles}
          count={count}
          total={facets?.total ?? 0}
          importing={imp.active}
          importDone={imp.done}
          importTotal={imp.total}
        />

        <div className="flex flex-1 overflow-hidden min-h-0">
          <div className="w-64 flex-shrink-0 border-r flex flex-col overflow-hidden">
            <FilterSidebar facets={facets} filter={filter} onChange={setFilter} />
          </div>

          <div className="flex-1 flex flex-col overflow-hidden min-w-0">
            <GalleryGrid designs={designs} hasAny={(facets?.total ?? 0) > 0} onSelect={setSelected} />
          </div>
        </div>

        {errors.length > 0 && (
          <div className="flex items-start gap-2 border-t bg-destructive/10 px-3 py-2 text-xs text-destructive">
            <div className="flex-1 space-y-0.5 max-h-20 overflow-auto">
              <p className="font-medium">{errors.length} arquivo(s) falharam ao importar:</p>
              {errors.map((e, i) => (
                <p key={i} className="font-mono opacity-80 truncate">{e}</p>
              ))}
            </div>
            <button onClick={() => setErrors([])} className="opacity-70 hover:opacity-100">
              <X className="h-4 w-4" />
            </button>
          </div>
        )}

        <DesignDetailDialog design={selected} onClose={() => setSelected(null)} />
      </div>
    </TooltipProvider>
  );
}
