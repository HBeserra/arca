import { useState, useEffect } from "react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ChatPanel } from "@/components/ChatPanel";
import { KnowledgeBasePanel } from "@/components/KnowledgeBasePanel";
import { ConfigPanel } from "@/components/ConfigPanel";
import { StatusBar } from "@/components/StatusBar";
import { cn } from "@/lib/utils";
import type { Message, AppConfig, Session } from "@/lib/types";
import * as IndexService from "../bindings/changeme/services/indexservice";

export default function App() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [activeSessionID, setActiveSessionID] = useState<string | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [isStreaming, setIsStreaming] = useState(false);
  const [configOpen, setConfigOpen] = useState(false);
  const [config, setConfig] = useState<AppConfig>({
    model: {
      model: "gpt-oss-20b",
      systemPrompt: "You are a helpful assistant. Answer questions using the provided context. If the context does not contain the answer, say so.",
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
      similarityThreshold: 0.3,
      embeddingModel: "embeddinggemma-300m",
      useReranker: false,
    },
  });

  // Load persisted sessions on mount.
  useEffect(() => {
    IndexService.ListSessions().then((list) => {
      if (list && list.length > 0) {
        const typed = list.filter(Boolean) as unknown as Session[];
        setSessions(typed);
        setActiveSessionID(typed[0].id);
      }
    });
  }, []);

  const activeSession = sessions.find((s) => s.id === activeSessionID) ?? null;

  const queryConfig = {
    topK: config.rag.topK,
    similarityThreshold: config.rag.similarityThreshold,
    useReranker: config.rag.useReranker,
    systemPrompt: config.model.systemPrompt,
    maxTokens: config.model.maxTokens,
  };

  function handleSessionCreated(sess: Session) {
    setSessions((prev) => [...prev, sess]);
  }

  function handleSessionDeleted(id: string) {
    setSessions((prev) => prev.filter((s) => s.id !== id));
    if (activeSessionID === id) {
      setActiveSessionID(sessions.find((s) => s.id !== id)?.id ?? null);
      setMessages([]);
    }
  }

  function handleSessionUpdated(sess: Session) {
    setSessions((prev) => prev.map((s) => (s.id === sess.id ? sess : s)));
  }

  function handleSessionSelect(id: string) {
    setActiveSessionID(id);
    setMessages([]); // clear chat when switching sessions
  }

  return (
    <TooltipProvider>
      <div className="flex flex-col h-screen w-screen overflow-hidden bg-background text-foreground">
        <div className="flex flex-1 overflow-hidden min-h-0">
          <div className="w-64 flex-shrink-0 border-r flex flex-col overflow-hidden">
            <KnowledgeBasePanel
              sessions={sessions}
              activeSessionID={activeSessionID}
              onSessionSelect={handleSessionSelect}
              onSessionCreated={handleSessionCreated}
              onSessionDeleted={handleSessionDeleted}
              onSessionUpdated={handleSessionUpdated}
            />
          </div>

          <div className="flex-1 flex flex-col overflow-hidden min-w-0">
            <ChatPanel
              messages={messages}
              setMessages={setMessages}
              isStreaming={isStreaming}
              setIsStreaming={setIsStreaming}
              activeSession={activeSession}
              queryConfig={queryConfig}
              onToggleConfig={() => setConfigOpen((o) => !o)}
              configOpen={configOpen}
            />
          </div>

          <div
            className={cn(
              "flex-shrink-0 border-l flex flex-col overflow-hidden transition-all duration-200",
              configOpen ? "w-72" : "w-0 border-l-0"
            )}
          >
            <ConfigPanel config={config} onChange={setConfig} />
          </div>
        </div>

        <StatusBar model={config.model.model} />
      </div>
    </TooltipProvider>
  );
}
