import { useEffect, useState } from "react";
import { Moon, Sun, Monitor, Circle } from "lucide-react";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

interface StatusBarProps {
  isRunning: boolean;
  model: string;
}

interface SystemStats {
  memoryUsed: number; // MB
  memoryTotal: number; // MB
  cpuPercent: number;
}

function useMockStats(): SystemStats {
  const [stats, setStats] = useState<SystemStats>({
    memoryUsed: 1240,
    memoryTotal: 16384,
    cpuPercent: 12,
  });

  useEffect(() => {
    const interval = setInterval(() => {
      setStats((prev) => ({
        memoryUsed: Math.max(800, prev.memoryUsed + Math.round((Math.random() - 0.5) * 80)),
        memoryTotal: prev.memoryTotal,
        cpuPercent: Math.min(99, Math.max(1, prev.cpuPercent + Math.round((Math.random() - 0.5) * 8))),
      }));
    }, 2000);
    return () => clearInterval(interval);
  }, []);

  return stats;
}

export function StatusBar({ isRunning, model }: StatusBarProps) {
  const { theme, resolvedTheme, setTheme } = useTheme();
  const stats = useMockStats();

  const memGB = (stats.memoryUsed / 1024).toFixed(1);
  const memTotalGB = (stats.memoryTotal / 1024).toFixed(0);
  const memPct = Math.round((stats.memoryUsed / stats.memoryTotal) * 100);

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
      <div className="flex items-center gap-4">
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
        </div>

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
          <span className="ml-1 opacity-60">({memPct}%)</span>
        </span>

        <Divider />

        {/* CPU */}
        <span>
          CPU&nbsp;
          <span className="text-foreground tabular-nums">{stats.cpuPercent}%</span>
        </span>
      </div>

      {/* Right: theme toggle */}
      <button
        onClick={cycleTheme}
        className="flex items-center gap-1.5 rounded px-1.5 py-0.5 hover:bg-accent hover:text-foreground transition-colors"
        title={`Theme: ${themeLabel} — click to cycle`}
      >
        <ThemeIcon className="h-3.5 w-3.5" />
        <span>{themeLabel}</span>
      </button>
    </div>
  );
}

function Divider() {
  return <span className="opacity-30">·</span>;
}
