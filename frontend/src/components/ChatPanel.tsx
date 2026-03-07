import { useRef, useEffect, useState } from "react";
import { Send, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ScrollArea } from "@/components/ui/scroll-area";
import { MessageBubble } from "./MessageBubble";
import type { Message, Citation, ChatTokenPayload, ChatCitationPayload, Session } from "@/lib/types";
import * as QueryService from "../../bindings/changeme/services/queryservice";
import { Events } from "@wailsio/runtime";

interface ChatPanelProps {
  messages: Message[];
  setMessages: React.Dispatch<React.SetStateAction<Message[]>>;
  isStreaming: boolean;
  setIsStreaming: React.Dispatch<React.SetStateAction<boolean>>;
  activeSession: Session | null;
  queryConfig: { topK: number; similarityThreshold: number; useReranker: boolean; systemPrompt: string; maxTokens: number };
}

export function ChatPanel({
  messages,
  setMessages,
  isStreaming,
  setIsStreaming,
  activeSession,
  queryConfig,
}: ChatPanelProps) {
  const [input, setInput] = useState("");
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  // Wire streaming events.
  useEffect(() => {
    const offToken = Events.On("chat:token", (e: { data: ChatTokenPayload }) => {
      setMessages((prev) =>
        prev.map((m) =>
          m.isStreaming ? { ...m, content: m.content + e.data.token } : m
        )
      );
    });

    const offCitation = Events.On("chat:citation", (e: { data: ChatCitationPayload }) => {
      const citations: Citation[] = e.data.citations ?? [];
      setMessages((prev) =>
        prev.map((m) => (m.isStreaming ? { ...m, citations } : m))
      );
    });

    const offDone = Events.On("chat:done", () => {
      setMessages((prev) =>
        prev.map((m) => (m.isStreaming ? { ...m, isStreaming: false } : m))
      );
      setIsStreaming(false);
    });

    const offError = Events.On("chat:error", (e: { data: { error: string } }) => {
      setMessages((prev) =>
        prev.map((m) =>
          m.isStreaming
            ? { ...m, isStreaming: false, content: `Error: ${e.data?.error ?? "unknown error"}` }
            : m
        )
      );
      setIsStreaming(false);
    });

    return () => {
      offToken();
      offCitation();
      offDone();
      offError();
    };
  }, [setMessages, setIsStreaming]);

  async function handleSend() {
    const trimmed = input.trim();
    if (!trimmed || isStreaming) return;

    setInput("");

    const userMessage: Message = {
      id: String(Date.now()),
      role: "user",
      content: trimmed,
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

    if (!activeSession) {
      // No session: show a hint.
      setMessages((prev) =>
        prev.map((m) =>
          m.isStreaming
            ? { ...m, isStreaming: false, content: "Please create or select a session in the left panel first." }
            : m
        )
      );
      setIsStreaming(false);
      return;
    }

    try {
      await QueryService.Query(activeSession.id, trimmed, {
        topK: queryConfig.topK,
        similarityThreshold: queryConfig.similarityThreshold,
        useReranker: queryConfig.useReranker,
        systemPrompt: queryConfig.systemPrompt,
        maxTokens: queryConfig.maxTokens,
      } as never);
    } catch (err) {
      setMessages((prev) =>
        prev.map((m) =>
          m.isStreaming
            ? { ...m, isStreaming: false, content: `Error: ${err}` }
            : m
        )
      );
      setIsStreaming(false);
    }
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      handleSend();
    }
  }

  function handleClearHistory() {
    if (activeSession) {
      QueryService.ClearHistory(activeSession.id);
    }
    setMessages([]);
  }

  const sessionName = activeSession?.name ?? "No session";

  return (
    <div className="flex flex-col h-full">
      <div
        className="border-b px-4 py-3 flex items-center justify-between"
        style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
      >
        <div style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}>
          <h2 className="font-semibold text-sm">Chat</h2>
          <p className="text-xs text-muted-foreground">{sessionName} · Cmd+Enter to send</p>
        </div>
        <div style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}>
          <Button size="sm" variant="ghost" className="h-7 w-7 p-0" onClick={handleClearHistory} title="Clear history">
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      <ScrollArea className="flex-1 px-4 py-4">
        {messages.map((message) => (
          <MessageBubble key={message.id} message={message} />
        ))}
        <div ref={bottomRef} />
      </ScrollArea>

      <div className="border-t p-4">
        <div className="flex gap-2 items-end">
          <Textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={
              activeSession
                ? `Ask anything about "${activeSession.name}"…`
                : "Select a session to start chatting…"
            }
            className="min-h-[80px] resize-none"
            disabled={isStreaming || !activeSession}
          />
          <Button
            onClick={handleSend}
            disabled={!input.trim() || isStreaming || !activeSession}
            size="icon"
            className="flex-shrink-0 h-10 w-10"
          >
            <Send className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}
