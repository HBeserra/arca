export type MessageRole = "user" | "assistant";

export interface Citation {
  id: string;
  documentId: string;
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
