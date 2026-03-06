export type MessageRole = "user" | "assistant";

export interface Citation {
  id: string;
  documentID: string;
  documentName: string;
  chunkIndex: number;
  excerpt: string;
  score: number;
}

export interface Message {
  id: string;
  role: MessageRole;
  content: string;
  citations?: Citation[];
  isStreaming?: boolean;
  timestamp: Date;
}

export type DocumentStatus = "indexed" | "processing" | "error";

export interface KBDocument {
  id: string;
  name: string;
  status: DocumentStatus;
  size: string;
  chunks: number;
  uploadedAt: Date;
}

// Session types (matching Wails bindings snake_case)
export interface SessionDocument {
  id: string;
  path: string;
  name: string;
  ext: string;
  size: number;
  status: DocumentStatus;
  chunk_count: number;
  summary: string;
}

export interface SessionGroup {
  id: string;
  label: string;
  doc_ids: string[];
}

export interface Session {
  id: string;
  name: string;
  created_at: string | null;
  documents: SessionDocument[];
  groups: SessionGroup[];
}

// Event payload types
export interface IndexProgressPayload {
  sessionID: string;
  docID: string;
  docName: string;
  stage: string;
  pct: number;
}

export interface IndexCompletePayload {
  sessionID: string;
}

export interface IndexErrorPayload {
  sessionID: string;
  docName: string;
  error: string;
}

export interface ChatTokenPayload {
  sessionID: string;
  token: string;
}

export interface ChatCitationPayload {
  sessionID: string;
  citations: Citation[];
}

export interface ChatDonePayload {
  sessionID: string;
}

export interface ModelConfig {
  model: string;
  systemPrompt: string;
  temperature: number;
  topP: number;
  maxTokens: number;
  presencePenalty: number;
  frequencyPenalty: number;
}

export interface RAGConfig {
  chunkSize: number;
  chunkOverlap: number;
  topK: number;
  similarityThreshold: number;
  embeddingModel: string;
  useReranker: boolean;
}

export interface AppConfig {
  model: ModelConfig;
  rag: RAGConfig;
}
