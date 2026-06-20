// Types mirroring the Go CatalogService JSON views (services/catalogservice.go).

export interface ColorInfo {
  r: number;
  g: number;
  b: number;
  hex: string;
  description: string;
}

export interface DesignInfo {
  id: string;
  fileName: string;
  path: string;
  format: string;
  widthMM: number;
  heightMM: number;
  stitchCount: number;
  colorChanges: number;
  colorCount: number;
  palette: ColorInfo[];
  thumbnailURL: string;
  fileSize: number;
  createdAt: string;
  // Phase 2 (empty until classification runs):
  caption: string;
  style: string;
  tags: string[];
  virtualFolderID: string;
}

export interface FacetCount {
  value: string;
  count: number;
}

export interface FacetInfo {
  total: number;
  formats: FacetCount[];
  maxSizeMM: number;
  maxStitches: number;
  maxColors: number;
}

export interface ListFilter {
  formats: string[];
  minSizeMM: number;
  maxSizeMM: number;
  minStitches: number;
  maxStitches: number;
  minColors: number;
  maxColors: number;
  search: string;
  limit: number;
  offset: number;
}

export const emptyFilter: ListFilter = {
  formats: [],
  minSizeMM: 0,
  maxSizeMM: 0,
  minStitches: 0,
  maxStitches: 0,
  minColors: 0,
  maxColors: 0,
  search: "",
  limit: 500,
  offset: 0,
};

// Wails event payloads (emitted by CatalogService).
export interface ImportProgressPayload {
  done: number;
  total: number;
  fileName: string;
  designID: string;
}

export interface ImportCompletePayload {
  total: number;
  imported: number;
  failed: number;
}

export interface ImportErrorPayload {
  fileName: string;
  error: string;
}
