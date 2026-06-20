import { FolderInput, FilePlus, Search, Moon, Sun, Monitor, Scissors, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

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
}

export function Toolbar(props: Props) {
  const { search, onSearch, onImportFolder, onImportFiles, count, total, importing, importDone, importTotal } = props;
  const pct = importTotal > 0 ? Math.min(100, Math.round((importDone / importTotal) * 100)) : 0;

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
            placeholder="Buscar por nome do arquivo…"
            className="h-8 pl-8 text-sm"
          />
        </div>

        <span className="text-xs tabular-nums text-muted-foreground">
          {count === total ? `${total} bordados` : `${count} de ${total}`}
        </span>

        <div className="ml-auto flex items-center gap-2">
          <Button variant="outline" size="sm" className="h-8" onClick={onImportFiles} disabled={importing}>
            <FilePlus className="h-4 w-4" /> Arquivos
          </Button>
          <Button size="sm" className="h-8" onClick={onImportFolder} disabled={importing}>
            <FolderInput className="h-4 w-4" /> Importar pasta
          </Button>
          <ThemeToggle />
        </div>
      </div>

      {importing && (
        <div className="flex items-center gap-2 px-3 pb-2">
          <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" />
          <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
            <div className="h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
          </div>
          <span className="text-xs tabular-nums text-muted-foreground">
            {importTotal > 0 ? `${importDone}/${importTotal}` : "preparando…"}
          </span>
        </div>
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
