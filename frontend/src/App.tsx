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
  VisionModelInfo,
  CaptionLanguageInfo,
  ExportProgressPayload,
  ExportErrorPayload,
  RestoreProgressPayload,
  RestoreErrorPayload,
} from "@/lib/types";
import * as CatalogService from "../bindings/stitchvault/services/catalogservice";

interface ImportState {
  active: boolean;
  done: number;
  total: number;
}

// Pagination: the gallery loads designs in pages (infinite scroll) so a 10k+
// catalog never builds 10k DOM nodes or fetches 10k rows at once. Semantic search
// returns a single ranked top-K batch (the backend can't offset-paginate it).
const PAGE_SIZE = 200;
const SEARCH_LIMIT = 300;

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
  const [visionInfo, setVisionInfo] = useState<VisionModelInfo | null>(null);
  const [captionInfo, setCaptionInfo] = useState<CaptionLanguageInfo | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [resetToken, setResetToken] = useState(0); // bump to scroll the gallery to top
  const [catalogIO, setCatalogIO] = useState<{ kind: "export" | "restore" | null; done: number; total: number }>({
    kind: null,
    done: 0,
    total: 0,
  });

  // Latest-value refs so the infinite-scroll callback and event handlers don't
  // capture stale state.
  const designsRef = useRef(designs);
  const countRef = useRef(count);
  const filterRef = useRef(filter);
  const loadingMoreRef = useRef(false);

  const hasMore = filter.search.trim() === "" && designs.length < count;

  // loadFirst replaces the result set with page 0 of the given filter.
  const loadFirst = useCallback((f: ListFilter) => {
    const searching = f.search.trim() !== "";
    const limit = searching ? SEARCH_LIMIT : PAGE_SIZE;
    CatalogService.ListDesigns({ ...f, limit, offset: 0 } as never).then((d) => {
      const list = (d ?? []) as unknown as DesignInfo[];
      setDesigns(list);
      if (searching) setCount(list.length);
    });
    if (!searching) CatalogService.CountDesigns(f as never).then((n) => setCount(n ?? 0));
  }, []);

  // loadMore appends the next page (browse only — semantic search is a single batch).
  const loadMore = useCallback(() => {
    if (loadingMoreRef.current) return;
    const f = filterRef.current;
    if (f.search.trim() !== "") return;
    if (designsRef.current.length >= countRef.current) return;
    loadingMoreRef.current = true;
    setLoadingMore(true);
    CatalogService.ListDesigns({ ...f, limit: PAGE_SIZE, offset: designsRef.current.length } as never)
      .then((d) => {
        const more = (d ?? []) as unknown as DesignInfo[];
        if (more.length) setDesigns((prev) => [...prev, ...more]);
      })
      .finally(() => {
        loadingMoreRef.current = false;
        setLoadingMore(false);
      });
  }, []);

  // reloadInPlace re-fetches the currently loaded window (after import/classify)
  // without resetting the scroll position.
  const reloadInPlace = useCallback(() => {
    const f = filterRef.current;
    const searching = f.search.trim() !== "";
    const limit = searching ? SEARCH_LIMIT : Math.max(PAGE_SIZE, designsRef.current.length);
    CatalogService.ListDesigns({ ...f, limit, offset: 0 } as never).then((d) => {
      const list = (d ?? []) as unknown as DesignInfo[];
      setDesigns(list);
      if (searching) setCount(list.length);
    });
    if (!searching) CatalogService.CountDesigns(f as never).then((n) => setCount(n ?? 0));
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

  const refreshVisionModels = useCallback(() => {
    CatalogService.VisionModels().then((v) => {
      if (v) setVisionInfo(v as unknown as VisionModelInfo);
    });
  }, []);

  const refreshCaptionLanguages = useCallback(() => {
    CatalogService.CaptionLanguages().then((c) => {
      if (c) setCaptionInfo(c as unknown as CaptionLanguageInfo);
    });
  }, []);

  // Facets, classify status, folders, models once on mount.
  useEffect(() => {
    refreshFacets();
    refreshClassifyStatus();
    refreshFolders();
    refreshVisionModels();
    refreshCaptionLanguages();
  }, [refreshFacets, refreshClassifyStatus, refreshFolders, refreshVisionModels, refreshCaptionLanguages]);

  // Reload page 0 and scroll to top whenever the filter changes (and on mount).
  useEffect(() => {
    loadFirst(filter);
    setResetToken((t) => t + 1);
  }, [filter, loadFirst]);

  // Keep latest refs for the one-time event subscription + infinite scroll.
  const reloadInPlaceRef = useRef(reloadInPlace);
  const refreshFacetsRef = useRef(refreshFacets);
  const refreshClassifyStatusRef = useRef(refreshClassifyStatus);
  const refreshFoldersRef = useRef(refreshFolders);
  useEffect(() => {
    designsRef.current = designs;
    countRef.current = count;
    filterRef.current = filter;
    reloadInPlaceRef.current = reloadInPlace;
    refreshFacetsRef.current = refreshFacets;
    refreshClassifyStatusRef.current = refreshClassifyStatus;
    refreshFoldersRef.current = refreshFolders;
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
      reloadInPlaceRef.current();
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
      reloadInPlaceRef.current();
      refreshClassifyStatusRef.current();
    });

    const offFComplete = Events.On("folders:complete", (_e: { data: FoldersCompletePayload }) => {
      setGeneratingFolders(false);
      refreshFoldersRef.current();
      reloadInPlaceRef.current();
    });
    const offFError = Events.On("folders:error", (e: { data: FoldersErrorPayload }) => {
      setGeneratingFolders(false);
      setErrors((prev) => [...prev.slice(-19), `pastas: ${e.data.error}`]);
    });

    const offExportProgress = Events.On("export:progress", (e: { data: ExportProgressPayload }) => {
      setCatalogIO({ kind: "export", done: e.data.done, total: e.data.total });
    });
    const offExportComplete = Events.On("export:complete", () => {
      setCatalogIO({ kind: null, done: 0, total: 0 });
    });
    const offExportError = Events.On("export:error", (e: { data: ExportErrorPayload }) => {
      setCatalogIO({ kind: null, done: 0, total: 0 });
      setErrors((prev) => [...prev.slice(-19), `exportar: ${e.data.error}`]);
    });
    const offRestoreProgress = Events.On("restore:progress", (e: { data: RestoreProgressPayload }) => {
      setCatalogIO({ kind: "restore", done: e.data.done, total: e.data.total });
    });
    const offRestoreComplete = Events.On("restore:complete", () => {
      setCatalogIO({ kind: null, done: 0, total: 0 });
      reloadInPlaceRef.current();
      refreshFacetsRef.current();
      refreshFoldersRef.current();
      refreshClassifyStatusRef.current();
    });
    const offRestoreError = Events.On("restore:error", (e: { data: RestoreErrorPayload }) => {
      setCatalogIO({ kind: null, done: 0, total: 0 });
      setErrors((prev) => [...prev.slice(-19), `importar: ${e.data.error}`]);
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
      offExportProgress();
      offExportComplete();
      offExportError();
      offRestoreProgress();
      offRestoreComplete();
      offRestoreError();
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

  const handleStopClassify = useCallback(() => {
    CatalogService.StopClassify();
  }, []);

  const handleSetVisionModel = useCallback(
    (id: string) => {
      CatalogService.SetVisionModel(id).then(() => {
        refreshVisionModels();
        refreshClassifyStatus();
      });
    },
    [refreshVisionModels, refreshClassifyStatus]
  );

  const handleEjectModels = useCallback(() => {
    CatalogService.EjectModels().then(() => refreshClassifyStatus());
  }, [refreshClassifyStatus]);

  const handleSetCaptionLanguage = useCallback(
    (code: string) => {
      CatalogService.SetCaptionLanguage(code).then(() => refreshCaptionLanguages());
    },
    [refreshCaptionLanguages]
  );

  const handleExportCatalog = useCallback(() => {
    setErrors([]);
    CatalogService.ExportCatalog();
  }, []);

  const handleImportCatalog = useCallback(() => {
    setErrors([]);
    CatalogService.ImportCatalog();
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
          onStopClassify={handleStopClassify}
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
              visionInfo={visionInfo}
              onSetVisionModel={handleSetVisionModel}
              captionInfo={captionInfo}
              onSetCaptionLanguage={handleSetCaptionLanguage}
              modelsLoaded={classifyStatus?.loaded ?? false}
              onEjectModels={handleEjectModels}
              aiBusy={classifying.active || generatingFolders}
              onExportCatalog={handleExportCatalog}
              onImportCatalog={handleImportCatalog}
              catalogIO={catalogIO}
            />
          </div>

          <div className="flex-1 flex flex-col overflow-hidden min-w-0">
            <GalleryGrid
              designs={designs}
              hasAny={(facets?.total ?? 0) > 0}
              onSelect={setSelected}
              onLoadMore={loadMore}
              hasMore={hasMore}
              loadingMore={loadingMore}
              resetToken={resetToken}
            />
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
          onDeleted={(id) => {
            setSelected(null);
            setDesigns((prev) => prev.filter((x) => x.id !== id));
            setCount((c) => Math.max(0, c - 1));
            refreshFacets();
            refreshClassifyStatus();
          }}
        />
      </div>
    </TooltipProvider>
  );
}
