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
  ClassifyStatus,
  ClassifyProgressPayload,
  ClassifyCompletePayload,
  ClassifyErrorPayload,
  FolderInfo,
  FoldersCompletePayload,
  FoldersErrorPayload,
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
  const [classifyStatus, setClassifyStatus] = useState<ClassifyStatus | null>(null);
  const [classifying, setClassifying] = useState<ImportState>({ active: false, done: 0, total: 0 });
  const [folders, setFolders] = useState<FolderInfo[]>([]);
  const [generatingFolders, setGeneratingFolders] = useState(false);

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

  const refreshClassifyStatus = useCallback(() => {
    CatalogService.ClassifyStatus().then((s) => {
      if (s) setClassifyStatus(s as unknown as ClassifyStatus);
    });
  }, []);

  const refreshFolders = useCallback(() => {
    CatalogService.ListFolders().then((f) => setFolders((f ?? []) as unknown as FolderInfo[]));
  }, []);

  // Facets, classify status, folders once on mount (refreshed after import/classify).
  useEffect(() => {
    refreshFacets();
    refreshClassifyStatus();
    refreshFolders();
  }, [refreshFacets, refreshClassifyStatus, refreshFolders]);

  // Designs whenever the filter changes (and on mount).
  useEffect(() => {
    refreshDesigns(filter);
  }, [filter, refreshDesigns]);

  // Keep latest refs for the one-time event subscription.
  const refreshDesignsRef = useRef(refreshDesigns);
  const refreshFacetsRef = useRef(refreshFacets);
  const refreshClassifyStatusRef = useRef(refreshClassifyStatus);
  const refreshFoldersRef = useRef(refreshFolders);
  const filterRef = useRef(filter);
  useEffect(() => {
    refreshDesignsRef.current = refreshDesigns;
    refreshFacetsRef.current = refreshFacets;
    refreshClassifyStatusRef.current = refreshClassifyStatus;
    refreshFoldersRef.current = refreshFolders;
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
      refreshClassifyStatusRef.current();
    });

    const offCProgress = Events.On("classify:progress", (e: { data: ClassifyProgressPayload }) => {
      setClassifying({ active: true, done: e.data.done, total: e.data.total });
    });
    const offCError = Events.On("classify:error", (e: { data: ClassifyErrorPayload }) => {
      setErrors((prev) => [...prev.slice(-19), `${e.data.fileName}: ${e.data.error}`]);
    });
    const offCComplete = Events.On("classify:complete", (e: { data: ClassifyCompletePayload }) => {
      setClassifying({ active: false, done: e.data.total, total: e.data.total });
      refreshDesignsRef.current(filterRef.current);
      refreshClassifyStatusRef.current();
    });

    const offFComplete = Events.On("folders:complete", (_e: { data: FoldersCompletePayload }) => {
      setGeneratingFolders(false);
      refreshFoldersRef.current();
      refreshDesignsRef.current(filterRef.current);
    });
    const offFError = Events.On("folders:error", (e: { data: FoldersErrorPayload }) => {
      setGeneratingFolders(false);
      setErrors((prev) => [...prev.slice(-19), `pastas: ${e.data.error}`]);
    });

    return () => {
      offProgress();
      offError();
      offComplete();
      offCProgress();
      offCError();
      offCComplete();
      offFComplete();
      offFError();
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

  const handleClassifyAll = useCallback(() => {
    setErrors([]);
    setClassifying({ active: true, done: 0, total: 0 });
    CatalogService.ClassifyAll();
  }, []);

  const handleGenerateFolders = useCallback(() => {
    setErrors([]);
    setGeneratingFolders(true);
    CatalogService.GenerateFolders(8);
  }, []);

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
          classifyStatus={classifyStatus}
          onClassify={handleClassifyAll}
          classifying={classifying.active}
          classifyDone={classifying.done}
          classifyTotal={classifying.total}
        />

        <div className="flex flex-1 overflow-hidden min-h-0">
          <div className="w-64 flex-shrink-0 border-r flex flex-col overflow-hidden">
            <FilterSidebar
              facets={facets}
              filter={filter}
              onChange={setFilter}
              folders={folders}
              foldersAvailable={(classifyStatus?.classified ?? 0) > 0}
              generatingFolders={generatingFolders}
              onGenerateFolders={handleGenerateFolders}
            />
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

        <DesignDetailDialog
          design={selected}
          classifyAvailable={classifyStatus?.available ?? false}
          onClose={() => setSelected(null)}
          onClassified={(d) => {
            setSelected(d);
            setDesigns((prev) => prev.map((x) => (x.id === d.id ? d : x)));
            refreshClassifyStatus();
          }}
        />
      </div>
    </TooltipProvider>
  );
}
