import type { Message, KBDocument, AppConfig } from "./types";

export const mockMessages: Message[] = [
  {
    id: "1",
    role: "user",
    content: "What are the key differences between RAG and fine-tuning for domain adaptation?",
    timestamp: new Date(Date.now() - 1000 * 60 * 5),
  },
  {
    id: "2",
    role: "assistant",
    content:
      "RAG and fine-tuning are complementary approaches for domain adaptation, each with distinct trade-offs:\n\n**RAG (Retrieval-Augmented Generation)** dynamically fetches relevant context at inference time from a knowledge base. It excels when:\n- Knowledge changes frequently\n- You need source attribution\n- You want to avoid catastrophic forgetting\n\n**Fine-tuning** bakes domain knowledge directly into model weights. It's preferable when:\n- You need consistent style or format\n- Latency is critical (no retrieval overhead)\n- The domain knowledge is stable\n\nHybrid approaches—using RAG over a fine-tuned model—often yield the best results for production systems.",
    citations: [
      {
        id: "c1",
        documentId: "doc1",
        documentName: "RAG Survey 2024.pdf",
        chunkIndex: 12,
        excerpt: "RAG systems dynamically retrieve relevant passages at inference time, enabling up-to-date knowledge without retraining.",
        score: 0.94,
      },
      {
        id: "c2",
        documentId: "doc2",
        documentName: "LLM Fine-tuning Guide.pdf",
        chunkIndex: 7,
        excerpt: "Fine-tuning directly modifies model parameters to encode domain-specific knowledge into the weights.",
        score: 0.87,
      },
    ],
    timestamp: new Date(Date.now() - 1000 * 60 * 4),
  },
  {
    id: "3",
    role: "user",
    content: "How does chunk size affect retrieval quality?",
    timestamp: new Date(Date.now() - 1000 * 60 * 2),
  },
  {
    id: "4",
    role: "assistant",
    content:
      "Chunk size is one of the most impactful RAG hyperparameters:\n\n- **Smaller chunks** (128–256 tokens): Higher precision, better semantic focus, but may lose context\n- **Larger chunks** (512–1024 tokens): More context preserved, but embeddings become less specific\n- **Sweet spot**: 256–512 tokens with ~20% overlap tends to work well for most document types\n\nConsider using hierarchical chunking or late chunking strategies for complex documents.",
    citations: [
      {
        id: "c3",
        documentId: "doc3",
        documentName: "Chunking Strategies.md",
        chunkIndex: 3,
        excerpt: "Optimal chunk size balances semantic coherence with embedding specificity. Most practitioners find 256–512 tokens effective.",
        score: 0.91,
      },
    ],
    timestamp: new Date(Date.now() - 1000 * 60 * 1),
  },
];

export const mockDocuments: KBDocument[] = [
  {
    id: "doc1",
    name: "RAG Survey 2024.pdf",
    status: "indexed",
    size: "2.4 MB",
    chunks: 142,
    uploadedAt: new Date(Date.now() - 1000 * 60 * 60 * 24 * 2),
  },
  {
    id: "doc2",
    name: "LLM Fine-tuning Guide.pdf",
    status: "indexed",
    size: "1.8 MB",
    chunks: 98,
    uploadedAt: new Date(Date.now() - 1000 * 60 * 60 * 24),
  },
  {
    id: "doc3",
    name: "Chunking Strategies.md",
    status: "indexed",
    size: "42 KB",
    chunks: 18,
    uploadedAt: new Date(Date.now() - 1000 * 60 * 60 * 12),
  },
  {
    id: "doc4",
    name: "Vector DB Comparison.pdf",
    status: "processing",
    size: "3.1 MB",
    chunks: 0,
    uploadedAt: new Date(Date.now() - 1000 * 60 * 30),
  },
  {
    id: "doc5",
    name: "Embeddings Benchmark.csv",
    status: "error",
    size: "890 KB",
    chunks: 0,
    uploadedAt: new Date(Date.now() - 1000 * 60 * 15),
  },
];

export const defaultConfig: AppConfig = {
  model: {
    model: "claude-sonnet-4-6",
    systemPrompt:
      "You are a helpful AI assistant with access to a knowledge base. Always cite your sources when answering questions based on retrieved documents. Be concise and accurate.",
    temperature: 0.7,
    topP: 0.9,
    maxTokens: 2048,
    presencePenalty: 0,
    frequencyPenalty: 0,
  },
  rag: {
    chunkSize: 512,
    chunkOverlap: 64,
    topK: 5,
    similarityThreshold: 0.7,
    embeddingModel: "text-embedding-3-small",
    useReranker: true,
  },
};

// IDs of documents cited in the last assistant message
export const activeSourceIds = ["doc1", "doc2", "doc3"];
