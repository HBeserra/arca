export type MessageRole = "user" | "assistant";

export interface ToolCall {
  name: string;
  args: Record<string, unknown>;
  result: string;
}

export interface Citation {
  id: string;
  documentID: string;
  documentName: string;
  chunkIndex: number;
  excerpt: string;
  score: number;
}

export interface ModelUsage {
  promptTokens: number;
  reasoningTokens: number;
  completionTokens: number;
  outputTokens: number;
  contextTokens: number;
  contextWindow: number;
  tokensPerSecond: number;
}

export interface Message {
  id: string;
  role: MessageRole;
  content: string;
  reasoning?: string;
  citations?: Citation[];
  usage?: ModelUsage;
  toolCalls?: ToolCall[];
  isStreaming?: boolean;
  timestamp: Date;
}

export type DocumentStatus = "waiting" | "processing" | "completed" | "error";

// Session types (matching Wails bindings / SessionInfo / DocumentInfo)
export interface SessionDocument {
  id: string;
  parent_id: string;   // empty string = root-level
  type: "file" | "folder";
  name: string;
  path: string;
  content_type: string;
  status: DocumentStatus;
}

export interface Session {
  id: string;
  name: string;
  created_at: string | null;
  documents: SessionDocument[];
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
  promptTokens: number;
  reasoningTokens: number;
  completionTokens: number;
  outputTokens: number;
  contextTokens: number;
  contextWindow: number;
  tokensPerSecond: number;
}

export interface ChatToolPayload {
  sessionID: string;
  name: string;
  args: Record<string, unknown>;
  result: string;
}


export interface ModelConfig {
  model: string;
  systemPrompt: string;
  language: string;
  temperature: number;
  topP: number;
  maxTokens: number;
  contextWindow: number; // model's total context window in tokens
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
