import { useEffect, useState } from "react";
import { Moon, Sun, Monitor, Circle, BotOff } from "lucide-react";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import * as MonitorService from "../../bindings/changeme/services/monitorservice";
import * as IndexService from "../../bindings/changeme/services/indexservice";

interface StatusBarProps {
  model: string;
  contextPct: number;
  contextTokens: number;
  contextWindow: number;
}

interface SystemStats {
  isRunning: boolean;
  memUsedBytes: number;
  memTotalBytes: number;
  cpuSystemPct: number;
}

function useSystemStats(): SystemStats {
  const [stats, setStats] = useState<SystemStats>({
    isRunning: false,
    memUsedBytes: 0,
    memTotalBytes: 0,
    cpuSystemPct: 0,
  });

  useEffect(() => {
    let cancelled = false;

    async function fetchStats() {
      try {
        const st = await MonitorService.GetStatus();
        if (cancelled || !st) return;
        setStats({
          isRunning: st.loadedModels,
          memUsedBytes: st.memory.usedBytes,
          memTotalBytes: st.memory.totalBytes,
          cpuSystemPct: st.cpu.systemUsagePct,
        });
      } catch {
        // silently ignore — stats are best-effort
      }
    }

    fetchStats();
    const interval = setInterval(fetchStats, 2000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  return stats;
}

export function StatusBar({ model, contextPct, contextTokens, contextWindow }: StatusBarProps) {
  const { theme, resolvedTheme, setTheme } = useTheme();
  const stats = useSystemStats();

  const isRunning = stats?.isRunning; // default to false if not provided

  const memGB = stats.memTotalBytes > 0
    ? (stats.memUsedBytes / 1024 / 1024 / 1024).toFixed(1)
    : "—";
  const memTotalGB = stats.memTotalBytes > 0
    ? (stats.memTotalBytes / 1024 / 1024 / 1024).toFixed(0)
    : "—";
  const memPct = stats.memTotalBytes > 0
    ? Math.round((stats.memUsedBytes / stats.memTotalBytes) * 100)
    : 0;
  const cpuPct = Math.round(stats.cpuSystemPct);

  function cycleTheme() {
    if (theme === "system") setTheme("light");
    else if (theme === "light") setTheme("dark");
    else setTheme("system");
  }

  const ThemeIcon = theme === "system" ? Monitor : resolvedTheme === "dark" ? Moon : Sun;
  const themeLabel = theme === "system" ? "System" : theme === "dark" ? "Dark" : "Light";

  return (
    <div className="h-7 flex-shrink-0 flex items-center justify-between px-6 border-t bg-muted/40 text-xs text-muted-foreground select-none">
      {/* Left: status indicators */}
      <div className="flex w-full items-center gap-4">
        {/* Running / Stopped */}
        <div className="flex items-center gap-1.5">
          <Circle
            className={cn(
              "h-2 w-2 fill-current",
              isRunning ? "text-green-500" : "text-muted-foreground/50"
            )}
          />
          <span className={isRunning ? "text-green-600 dark:text-green-400" : ""}>
            {isRunning ? "Running" : "Idle"}
          </span>
          <button
            onClick={() => IndexService.Eject()}
            className="flex items-center gap-1 rounded px-1.5 py-0.5 hover:bg-accent hover:text-foreground transition-colors"
            title="Eject models"
          >
            <BotOff className="h-3.5 w-3.5" />
          </button>
        </div>

        {/* Right: eject + theme toggle */}
        <div className="flex items-center gap-2">
          

          <Divider />

          {/* Model */}
          <span className="truncate max-w-[140px]" title={model}>
            {model}
          </span>

          <Divider />

          {/* Memory */}
          <span>
            Mem&nbsp;
            <span className="text-foreground tabular-nums">
              {memGB}&nbsp;/&nbsp;{memTotalGB} GB
            </span>
            {stats.memTotalBytes > 0 && (
              <span className="ml-1 opacity-60">({memPct}%)</span>
            )}
          </span>

          <Divider />

          {/* CPU */}
          <span>
            CPU&nbsp;
            <span className="text-foreground tabular-nums">
              {stats.memTotalBytes > 0 ? `${cpuPct}%` : "—"}
            </span>
          </span>

          <Divider />

          {/* Context window */}
          <span
            title={`~${contextTokens.toLocaleString()} / ${contextWindow.toLocaleString()} tokens`}
            className={cn(
              contextPct >= 60 ? "text-red-500" :
              contextPct >= 30 ? "text-yellow-500" :
              ""
            )}
          >
            Ctx&nbsp;
            <span className="tabular-nums font-medium">{contextPct}%</span>
          </span>
        </div>

        <div className="flex-1" />

        <button
          onClick={cycleTheme}
          className="flex items-center gap-1.5 rounded px-1.5 py-0.5 hover:bg-accent hover:text-foreground transition-colors"
          title={`Theme: ${themeLabel} — click to cycle`}
        >
          <ThemeIcon className="h-3.5 w-3.5" />
          <span>{themeLabel}</span>
        </button>
      </div>
    </div>
  );
}

function Divider() {
  return <span className="opacity-30">|</span>;
}
