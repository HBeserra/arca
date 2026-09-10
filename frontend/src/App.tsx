import { useState, useEffect, useCallback, useRef } from "react";
import { Events } from "@wailsio/runtime";
import { X, Trash2, RotateCw } from "lucide-react";
import { Button } from "@/components/ui/button";
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
  WorkerModeInfo,
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

interface UIError {
  kind: "import" | "classify" | "folder" | "export" | "restore" | "general";
  text: string;
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
  const [errors, setErrors] = useState<UIError[]>([]);
  const [classifyStatus, setClassifyStatus] = useState<ClassifyStatus | null>(null);
  const [classifying, setClassifying] = useState<ImportState>({ active: false, done: 0, total: 0 });
  const [folders, setFolders] = useState<FolderInfo[]>([]);
  const [generatingFolders, setGeneratingFolders] = useState(false);
  const [visionInfo, setVisionInfo] = useState<VisionModelInfo | null>(null);
  const [captionInfo, setCaptionInfo] = useState<CaptionLanguageInfo | null>(null);
  const [workerInfo, setWorkerInfo] = useState<WorkerModeInfo | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [resetToken, setResetToken] = useState(0); // bump to scroll the gallery to top
  const [catalogIO, setCatalogIO] = useState<{ kind: "export" | "restore" | null; done: number; total: number }>({
    kind: null,
    done: 0,
    total: 0,
  });
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [confirmBulkDelete, setConfirmBulkDelete] = useState(false);

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

  const refreshWorkerModes = useCallback(() => {
    CatalogService.WorkerModes().then((w) => {
      if (w) setWorkerInfo(w as unknown as WorkerModeInfo);
    });
  }, []);

  const handleSetWorkerMode = useCallback(
    (id: string) => {
      CatalogService.SetWorkerMode(id).then(() => {
        refreshWorkerModes();
      });
    },
    [refreshWorkerModes],
  );

  // Facets, classify status, folders, models once on mount.
  useEffect(() => {
    refreshFacets();
    refreshClassifyStatus();
    refreshFolders();
    refreshVisionModels();
    refreshCaptionLanguages();
    refreshWorkerModes();
  }, [refreshFacets, refreshClassifyStatus, refreshFolders, refreshVisionModels, refreshCaptionLanguages, refreshWorkerModes]);

  // Reload page 0 and scroll to top whenever the filter changes (and on mount).
  useEffect(() => {
    loadFirst(filter);
    setResetToken((t) => t + 1);
    setSelectedIds(new Set());
    setConfirmBulkDelete(false);
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
      setErrors((prev) => [...prev.slice(-19), { kind: "import", text: `${e.data.fileName}: ${e.data.error}` }]);
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
      setErrors((prev) => [...prev.slice(-19), { kind: "classify", text: `${e.data.fileName}: ${e.data.error}` }]);
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
      setErrors((prev) => [...prev.slice(-19), { kind: "folder", text: `pastas: ${e.data.error}` }]);
    });

    const offExportProgress = Events.On("export:progress", (e: { data: ExportProgressPayload }) => {
      setCatalogIO({ kind: "export", done: e.data.done, total: e.data.total });
    });
    const offExportComplete = Events.On("export:complete", () => {
      setCatalogIO({ kind: null, done: 0, total: 0 });
    });
    const offExportError = Events.On("export:error", (e: { data: ExportErrorPayload }) => {
      setCatalogIO({ kind: null, done: 0, total: 0 });
      setErrors((prev) => [...prev.slice(-19), { kind: "export", text: `exportar: ${e.data.error}` }]);
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
      setErrors((prev) => [...prev.slice(-19), { kind: "restore", text: `importar: ${e.data.error}` }]);
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

  const toggleSelect = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const clearSelection = useCallback(() => {
    setSelectedIds(new Set());
    setConfirmBulkDelete(false);
  }, []);

  const selectAllLoaded = useCallback(() => {
    setSelectedIds(new Set(designs.map((d) => d.id)));
  }, [designs]);

  const handleBulkDelete = useCallback(() => {
    const ids = [...selectedIds];
    if (ids.length === 0) return;
    const removed = new Set(ids);
    CatalogService.DeleteDesigns(ids as never).then(() => {
      setDesigns((prev) => prev.filter((d) => !removed.has(d.id)));
      setCount((c) => Math.max(0, c - ids.length));
      setSelectedIds(new Set());
      setConfirmBulkDelete(false);
      refreshFacets();
      refreshClassifyStatus();
    });
  }, [selectedIds, refreshFacets, refreshClassifyStatus]);

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

        {selectedIds.size > 0 && (
          <div className="flex items-center gap-3 border-b bg-accent/40 px-3 py-1.5 text-sm">
            <span className="font-medium">{selectedIds.size} selecionado(s)</span>
            <button className="text-xs text-muted-foreground hover:text-foreground" onClick={selectAllLoaded}>
              Selecionar carregados
            </button>
            <button className="text-xs text-muted-foreground hover:text-foreground" onClick={clearSelection}>
              Limpar
            </button>
            <div className="ml-auto flex items-center gap-2">
              {confirmBulkDelete ? (
                <>
                  <span className="text-xs text-muted-foreground">Remover {selectedIds.size}?</span>
                  <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => setConfirmBulkDelete(false)}>
                    Cancelar
                  </Button>
                  <Button variant="destructive" size="sm" className="h-7 text-xs" onClick={handleBulkDelete}>
                    <Trash2 className="h-3.5 w-3.5" /> Remover
                  </Button>
                </>
              ) : (
                <Button variant="destructive" size="sm" className="h-7 text-xs" onClick={() => setConfirmBulkDelete(true)}>
                  <Trash2 className="h-3.5 w-3.5" /> Remover selecionados
                </Button>
              )}
            </div>
          </div>
        )}

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
              workerInfo={workerInfo}
              onSetWorkerMode={handleSetWorkerMode}
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
              selectedIds={selectedIds}
              onToggleSelect={toggleSelect}
            />
          </div>
        </div>

        {errors.length > 0 && (() => {
          const hasClassifyErrors = errors.some((e) => e.kind === "classify");
          const allClassify = errors.every((e) => e.kind === "classify");
          const allImport = errors.every((e) => e.kind === "import");

          let title = `${errors.length} erro(s) encontrados:`;
          if (allClassify) {
            title = `${errors.length} bordado(s) falharam ao classificar:`;
          } else if (allImport) {
            title = `${errors.length} arquivo(s) falharam ao importar:`;
          }

          return (
            <div className="flex items-start gap-2 border-t bg-destructive/10 px-3 py-2 text-xs text-destructive">
              <div className="flex-1 space-y-0.5 max-h-20 overflow-auto">
                <p className="font-medium">{title}</p>
                {errors.map((e, i) => (
                  <p key={i} className="font-mono opacity-80 truncate">{e.text}</p>
                ))}
              </div>
              {hasClassifyErrors && !classifying.active && (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-6 text-xs gap-1 border-destructive/40 text-destructive hover:bg-destructive/10 self-center"
                  onClick={() => {
                    setErrors((prev) => prev.filter((e) => e.kind !== "classify"));
                    handleClassifyAll();
                  }}
                >
                  <RotateCw className="h-3 w-3" /> Tentar novamente
                </Button>
              )}
              <button onClick={() => setErrors([])} className="opacity-70 hover:opacity-100 self-center">
                <X className="h-4 w-4" />
              </button>
            </div>
          );
        })()}

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
