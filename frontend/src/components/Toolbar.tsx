import { FolderInput, FilePlus, Search, Moon, Sun, Monitor, Scissors, Loader2, Sparkles, Square } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import type { ClassifyStatus } from "@/lib/types";

interface Props {
  search: string;
  onSearch: (s: string) => void;
  onImportFolder: () => void;
  onImportFiles: () => void;
  count: number;
  total: number;
  importing: boolean;
  importDone: number;
  importTotal: number;
  classifyStatus: ClassifyStatus | null;
  onClassify: () => void;
  onStopClassify: () => void;
  classifying: boolean;
  classifyDone: number;
  classifyTotal: number;
}

export function Toolbar(props: Props) {
  const {
    search, onSearch, onImportFolder, onImportFiles, count, total,
    importing, importDone, importTotal,
    classifyStatus, onClassify, onStopClassify, classifying, classifyDone, classifyTotal,
  } = props;

  const semantic = (classifyStatus?.classified ?? 0) > 0;
  const allClassified =
    classifyStatus !== null && classifyStatus.total > 0 && classifyStatus.classified >= classifyStatus.total;

  return (
    <div className="flex-shrink-0 border-b">
      <div className="flex items-center gap-3 px-3 h-12">
        <div className="flex items-center gap-1.5 font-semibold text-sm select-none">
          <Scissors className="h-4 w-4 text-primary" />
          StitchVault
        </div>

        <div className="relative ml-2 max-w-xs flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => onSearch(e.target.value)}
            placeholder={semantic ? "Busca semântica (o que o bordado mostra)…" : "Buscar por nome do arquivo…"}
            className="h-8 pl-8 text-sm"
          />
        </div>

        <span className="text-xs tabular-nums text-muted-foreground">
          {count === total ? `${total} bordados` : `${count} de ${total}`}
        </span>

        <div className="ml-auto flex items-center gap-2">
          {classifyStatus?.available && (
            <Button
              variant="outline"
              size="sm"
              className="h-8"
              onClick={onClassify}
              disabled={classifying || importing || allClassified || (classifyStatus.total ?? 0) === 0}
              title="Classificar bordados com o modelo de visão local"
            >
              <Sparkles className="h-4 w-4" />
              Classificar IA
              {classifyStatus.total > 0 && (
                <span className="tabular-nums text-muted-foreground">
                  {classifyStatus.classified}/{classifyStatus.total}
                </span>
              )}
            </Button>
          )}
          <Button variant="outline" size="sm" className="h-8" onClick={onImportFiles} disabled={importing || classifying}>
            <FilePlus className="h-4 w-4" /> Arquivos
          </Button>
          <Button size="sm" className="h-8" onClick={onImportFolder} disabled={importing || classifying}>
            <FolderInput className="h-4 w-4" /> Importar pasta
          </Button>
          <ThemeToggle />
        </div>
      </div>

      {importing && <ProgressRow label="Importando" done={importDone} total={importTotal} />}
      {classifying && (
        <ProgressRow label="Classificando" done={classifyDone} total={classifyTotal} onStop={onStopClassify} />
      )}
    </div>
  );
}

function ProgressRow({
  label,
  done,
  total,
  onStop,
}: {
  label: string;
  done: number;
  total: number;
  onStop?: () => void;
}) {
  const pct = total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0;
  return (
    <div className="flex items-center gap-2 px-3 pb-2">
      <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" />
      <span className="text-xs text-muted-foreground w-24">{label}…</span>
      <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
        <div className="h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
      </div>
      <span className="text-xs tabular-nums text-muted-foreground">
        {total > 0 ? `${done}/${total}` : "preparando…"}
      </span>
      {onStop && (
        <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={onStop}>
          <Square className="h-3 w-3" /> Parar
        </Button>
      )}
    </div>
  );
}

function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  const next = theme === "system" ? "light" : theme === "light" ? "dark" : "system";
  const Icon = theme === "system" ? Monitor : theme === "light" ? Sun : Moon;
  return (
    <Button
      variant="ghost"
      size="icon"
      className={cn("h-8 w-8")}
      title={`Tema: ${theme}`}
      onClick={() => setTheme(next)}
    >
      <Icon className="h-4 w-4" />
    </Button>
  );
}
