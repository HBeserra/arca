import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { Message, ModelUsage } from "@/lib/types";
import { cn } from "@/lib/utils";

function UsageBar({ usage }: { usage: ModelUsage }) {
  const ctxPct = usage.contextWindow > 0
    ? Math.min(100, Math.round((usage.contextTokens / usage.contextWindow) * 100))
    : 0;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex items-center gap-1.5 cursor-default">
          <div className="flex items-center gap-1 text-xs text-muted-foreground/70">
            <span>{usage.tokensPerSecond.toFixed(1)} t/s</span>
            <span className="text-muted-foreground/40">·</span>
            <span>{usage.contextTokens.toLocaleString()}/{usage.contextWindow.toLocaleString()}</span>
          </div>
          <div className="w-12 h-1 rounded-full bg-muted overflow-hidden">
            <div
              className={cn("h-full rounded-full", ctxPct > 85 ? "bg-red-500" : ctxPct > 60 ? "bg-yellow-500" : "bg-primary/50")}
              style={{ width: `${ctxPct}%` }}
            />
          </div>
        </div>
      </TooltipTrigger>
      <TooltipContent className="text-xs space-y-0.5">
        <p className="font-semibold mb-1">Model usage</p>
        <p>Prompt: {usage.promptTokens.toLocaleString()} tokens</p>
        {usage.reasoningTokens > 0 && <p>Reasoning: {usage.reasoningTokens.toLocaleString()} tokens</p>}
        <p>Completion: {usage.completionTokens.toLocaleString()} tokens</p>
        <p>Context: {usage.contextTokens.toLocaleString()} / {usage.contextWindow.toLocaleString()} ({ctxPct}%)</p>
        <p>Speed: {usage.tokensPerSecond.toFixed(1)} tokens/s</p>
      </TooltipContent>
    </Tooltip>
  );
}

interface MessageBubbleProps {
  message: Message;
}

function renderContent(content: string) {
  const lines = content.split("\n");
  return lines.map((line, i) => {
    const parts = line.split(/(\*\*[^*]+\*\*)/g);
    return (
      <p key={i} className={i > 0 ? "mt-2" : ""}>
        {parts.map((part, j) => {
          if (part.startsWith("**") && part.endsWith("**")) {
            return <strong key={j}>{part.slice(2, -2)}</strong>;
          }
          if (part.startsWith("- ")) {
            return <span key={j} className="block ml-3">• {part.slice(2)}</span>;
          }
          return <span key={j}>{part}</span>;
        })}
      </p>
    );
  });
}

function ReasoningBlock({ reasoning, isStreaming }: { reasoning: string; isStreaming?: boolean }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="mb-2 rounded-lg border border-border/50 bg-muted/40 text-xs overflow-hidden">
      <button
        className="flex w-full items-center gap-1.5 px-3 py-1.5 text-muted-foreground hover:text-foreground transition-colors"
        onClick={() => setExpanded((v) => !v)}
      >
        {expanded
          ? <ChevronDown className="h-3 w-3 flex-shrink-0" />
          : <ChevronRight className="h-3 w-3 flex-shrink-0" />}
        <span className="font-medium italic">
          {isStreaming ? "Thinking…" : "Thought process"}
        </span>
        {isStreaming && !expanded && (
          <span className="inline-flex gap-0.5 ml-1">
            <span className="animate-bounce w-1 h-1 rounded-full bg-current [animation-delay:0ms]" />
            <span className="animate-bounce w-1 h-1 rounded-full bg-current [animation-delay:150ms]" />
            <span className="animate-bounce w-1 h-1 rounded-full bg-current [animation-delay:300ms]" />
          </span>
        )}
      </button>
      {expanded && (
        <div className="px-3 pb-2 text-muted-foreground italic leading-relaxed whitespace-pre-wrap break-words">
          {reasoning}
          {isStreaming && (
            <span className="inline-flex gap-0.5 ml-1 align-middle">
              <span className="animate-bounce w-1 h-1 rounded-full bg-current [animation-delay:0ms]" />
              <span className="animate-bounce w-1 h-1 rounded-full bg-current [animation-delay:150ms]" />
              <span className="animate-bounce w-1 h-1 rounded-full bg-current [animation-delay:300ms]" />
            </span>
          )}
        </div>
      )}
    </div>
  );
}

const CITATIONS_VISIBLE = 3;

function CitationsBlock({ citations }: { citations: NonNullable<Message["citations"]> }) {
  const [showAll, setShowAll] = useState(false);
  const visible = showAll ? citations : citations.slice(0, CITATIONS_VISIBLE);
  const hidden = citations.length - CITATIONS_VISIBLE;

  return (
    <div className="mt-2 px-1">
      <div className="flex flex-wrap gap-1.5">
        {visible.map((citation) => (
          <Tooltip key={citation.id}>
            <TooltipTrigger asChild>
              <div>
                <Badge variant="outline" className="cursor-pointer text-xs hover:bg-accent">
                  {citation.documentName} · {Math.round(citation.score * 100)}%
                </Badge>
              </div>
            </TooltipTrigger>
            <TooltipContent className="max-w-xs">
              <p className="font-semibold mb-1">{citation.documentName}</p>
              <p className="text-xs opacity-90 italic">"{citation.excerpt}"</p>
            </TooltipContent>
          </Tooltip>
        ))}
        {!showAll && hidden > 0 && (
          <button
            onClick={() => setShowAll(true)}
            className="text-xs text-muted-foreground hover:text-foreground transition-colors px-1"
          >
            +{hidden} more
          </button>
        )}
        {showAll && hidden > 0 && (
          <button
            onClick={() => setShowAll(false)}
            className="text-xs text-muted-foreground hover:text-foreground transition-colors px-1"
          >
            Show less
          </button>
        )}
      </div>
    </div>
  );
}

export function MessageBubble({ message }: MessageBubbleProps) {
  const isUser = message.role === "user";

  return (
    <div className={cn("flex gap-3 mb-4", isUser ? "justify-end" : "justify-start")}>
      {!isUser && (
        <div className="flex-shrink-0 w-7 h-7 rounded-full bg-primary flex items-center justify-center text-primary-foreground text-xs font-bold mt-1">
          AI
        </div>
      )}
      <div className={cn("max-w-[75%]", isUser ? "items-end" : "items-start")}>
        {!isUser && message.reasoning && (
          <ReasoningBlock reasoning={message.reasoning} isStreaming={message.isStreaming} />
        )}
        { (message.content || (!message.reasoning && (message.citations?.length  || 0) > 0)) && 
          <div
          className={cn(
            "rounded-2xl px-4 py-3 text-sm leading-relaxed",
            isUser
              ? "bg-primary text-primary-foreground rounded-tr-sm"
              : "bg-muted text-foreground rounded-tl-sm"
          )}
        >
          {renderContent(message.content)}
          {message.isStreaming && !message.reasoning && (
            <span className="inline-flex gap-1 ml-1">
              <span className="animate-bounce w-1 h-1 rounded-full bg-current [animation-delay:0ms]" />
              <span className="animate-bounce w-1 h-1 rounded-full bg-current [animation-delay:150ms]" />
              <span className="animate-bounce w-1 h-1 rounded-full bg-current [animation-delay:300ms]" />
            </span>
          )}
        </div>
        }
        {message.citations && message.citations.length > 0 && (
          <CitationsBlock citations={message.citations} />
        )}
        <div className="flex items-center gap-2 mt-1 px-1 flex-wrap">
          <p className="text-xs text-muted-foreground">
            {message.timestamp.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
          </p>
          {!isUser && message.usage && !message.isStreaming && (
            <UsageBar usage={message.usage} />
          )}
        </div>
      </div>
      {isUser && (
        <div className="flex-shrink-0 w-7 h-7 rounded-full bg-secondary flex items-center justify-center text-secondary-foreground text-xs font-bold mt-1">
          U
        </div>
      )}
    </div>
  );
}
