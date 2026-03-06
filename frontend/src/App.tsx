import { useState } from "react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ChatPanel } from "@/components/ChatPanel";
import { KnowledgeBasePanel } from "@/components/KnowledgeBasePanel";
import { ConfigPanel } from "@/components/ConfigPanel";
import { StatusBar } from "@/components/StatusBar";
import { mockMessages, mockDocuments, defaultConfig, activeSourceIds } from "@/lib/mock-data";
import type { Message, AppConfig } from "@/lib/types";

export default function App() {
  const [messages, setMessages] = useState<Message[]>(mockMessages);
  const [isStreaming, setIsStreaming] = useState(false);
  const [config, setConfig] = useState<AppConfig>(defaultConfig);

  function handleSend(content: string) {
    const userMessage: Message = {
      id: String(Date.now()),
      role: "user",
      content,
      timestamp: new Date(),
    };

    const streamingMessage: Message = {
      id: String(Date.now() + 1),
      role: "assistant",
      content: "",
      isStreaming: true,
      timestamp: new Date(),
    };

    setMessages((prev) => [...prev, userMessage, streamingMessage]);
    setIsStreaming(true);

    const response =
      "This is a mock response. In a real implementation, this would stream content from the backend via Wails bindings, retrieving relevant chunks from the knowledge base and generating a response using the configured model.";

    let i = 0;
    const interval = setInterval(() => {
      i += 3;
      const chunk = response.slice(0, i);
      const done = i >= response.length;

      setMessages((prev) =>
        prev.map((m) =>
          m.isStreaming ? { ...m, content: chunk, isStreaming: !done } : m
        )
      );

      if (done) {
        clearInterval(interval);
        setIsStreaming(false);
      }
    }, 30);
  }

  return (
    <TooltipProvider>
      <div className="flex flex-col h-screen w-screen overflow-hidden bg-background text-foreground">
        {/* Three-column layout — headers serve as the drag region */}
        <div className="flex flex-1 overflow-hidden min-h-0">
          <div className="w-64 flex-shrink-0 border-r flex flex-col overflow-hidden">
            <KnowledgeBasePanel documents={mockDocuments} activeSourceIds={activeSourceIds} />
          </div>

          <div className="flex-1 flex flex-col overflow-hidden min-w-0">
            <ChatPanel messages={messages} onSend={handleSend} isStreaming={isStreaming} />
          </div>

          <div className="w-72 flex-shrink-0 border-l flex flex-col overflow-hidden">
            <ConfigPanel config={config} onChange={setConfig} />
          </div>
        </div>

        {/* Status bar */}
        <StatusBar isRunning={isStreaming} model={config.model.model} />
      </div>
    </TooltipProvider>
  );
}
