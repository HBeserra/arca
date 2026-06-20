import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Label } from "@/components/ui/label";
import { Slider } from "@/components/ui/slider";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { AppConfig } from "@/lib/types";

interface ConfigPanelProps {
  config: AppConfig;
  onChange: (config: AppConfig) => void;
}

export function ConfigPanel({ config, onChange }: ConfigPanelProps) {
  function updateModel<K extends keyof AppConfig["model"]>(key: K, value: AppConfig["model"][K]) {
    onChange({ ...config, model: { ...config.model, [key]: value } });
  }

  function updateRAG<K extends keyof AppConfig["rag"]>(key: K, value: AppConfig["rag"][K]) {
    onChange({ ...config, rag: { ...config.rag, [key]: value } });
  }

  return (
    <div className="flex flex-col h-full">
      <div
        className="border-b px-4 py-3"
        style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
      >
        <h2 className="font-semibold text-sm" style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}>Configuration</h2>
        <p className="text-xs text-muted-foreground" style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}>Model & RAG settings</p>
      </div>

      <Tabs defaultValue="model" className="flex-1 flex flex-col min-h-0">
        <div className="px-3 pt-3">
          <TabsList className="w-full grid grid-cols-3">
            <TabsTrigger value="model">Model</TabsTrigger>
            <TabsTrigger value="params">Params</TabsTrigger>
            <TabsTrigger value="rag">RAG</TabsTrigger>
          </TabsList>
        </div>

        <ScrollArea className="flex-1">
          <div className="px-4 pb-4">
            {/* Model Tab */}
            <TabsContent value="model" className="space-y-4">
              <div className="space-y-1.5">
                <Label>Model</Label>
                <Select value={config.model.model} onValueChange={(v) => updateModel("model", v)}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="claude-sonnet-4-6">Claude Sonnet 4.6</SelectItem>
                    <SelectItem value="claude-opus-4-6">Claude Opus 4.6</SelectItem>
                    <SelectItem value="claude-haiku-4-5">Claude Haiku 4.5</SelectItem>
                    <SelectItem value="gpt-4o">GPT-4o</SelectItem>
                    <SelectItem value="gpt-4o-mini">GPT-4o Mini</SelectItem>
                    <SelectItem value="llama3-70b">Llama 3 70B</SelectItem>
                    <SelectItem value="mistral-large">Mistral Large</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-1.5">
                <Label>Response Language</Label>
                <Select value={config.model.language} onValueChange={(v) => updateModel("language", v)}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="English">English</SelectItem>
                    <SelectItem value="Portuguese">Portuguese</SelectItem>
                    <SelectItem value="Spanish">Spanish</SelectItem>
                    <SelectItem value="French">French</SelectItem>
                    <SelectItem value="German">German</SelectItem>
                    <SelectItem value="Italian">Italian</SelectItem>
                    <SelectItem value="Japanese">Japanese</SelectItem>
                    <SelectItem value="Chinese">Chinese</SelectItem>
                    <SelectItem value="Korean">Korean</SelectItem>
                    <SelectItem value="Russian">Russian</SelectItem>
                    <SelectItem value="Arabic">Arabic</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-1.5">
                <Label>System Prompt</Label>
                <Textarea
                  value={config.model.systemPrompt}
                  onChange={(e) => updateModel("systemPrompt", e.target.value)}
                  className="min-h-[140px] text-xs resize-none"
                  placeholder="Enter system prompt..."
                />
              </div>
            </TabsContent>

            {/* Parameters Tab */}
            <TabsContent value="params" className="space-y-5">
              <SliderField
                label="Temperature"
                value={config.model.temperature}
                min={0}
                max={2}
                step={0.01}
                onChange={(v) => updateModel("temperature", v)}
              />
              <SliderField
                label="Top-P"
                value={config.model.topP}
                min={0}
                max={1}
                step={0.01}
                onChange={(v) => updateModel("topP", v)}
              />
              <div className="space-y-1.5">
                <Label>Max Tokens</Label>
                <Input
                  type="number"
                  value={config.model.maxTokens}
                  onChange={(e) => updateModel("maxTokens", Number(e.target.value))}
                  min={1}
                  max={32768}
                />
              </div>
              <div className="space-y-1.5">
                <Label>Context Window</Label>
                <Input
                  type="number"
                  value={config.model.contextWindow}
                  onChange={(e) => updateModel("contextWindow", Number(e.target.value))}
                  min={512}
                  max={1048576}
                />
              </div>
              <SliderField
                label="Presence Penalty"
                value={config.model.presencePenalty}
                min={-2}
                max={2}
                step={0.01}
                onChange={(v) => updateModel("presencePenalty", v)}
              />
              <SliderField
                label="Frequency Penalty"
                value={config.model.frequencyPenalty}
                min={-2}
                max={2}
                step={0.01}
                onChange={(v) => updateModel("frequencyPenalty", v)}
              />
            </TabsContent>

            {/* RAG Tab */}
            <TabsContent value="rag" className="space-y-5">
              <div className="space-y-1.5">
                <Label>Chunk Size (tokens)</Label>
                <Input
                  type="number"
                  value={config.rag.chunkSize}
                  onChange={(e) => updateRAG("chunkSize", Number(e.target.value))}
                  min={64}
                  max={4096}
                />
              </div>
              <div className="space-y-1.5">
                <Label>Chunk Overlap (tokens)</Label>
                <Input
                  type="number"
                  value={config.rag.chunkOverlap}
                  onChange={(e) => updateRAG("chunkOverlap", Number(e.target.value))}
                  min={0}
                  max={512}
                />
              </div>
              <div className="space-y-1.5">
                <Label>Top-K Retrieval</Label>
                <Input
                  type="number"
                  value={config.rag.topK}
                  onChange={(e) => updateRAG("topK", Number(e.target.value))}
                  min={1}
                  max={20}
                />
              </div>
              <SliderField
                label="Similarity Threshold"
                value={config.rag.similarityThreshold}
                min={0}
                max={1}
                step={0.01}
                onChange={(v) => updateRAG("similarityThreshold", v)}
              />
              <div className="space-y-1.5">
                <Label>Embedding Model</Label>
                <Select
                  value={config.rag.embeddingModel}
                  onValueChange={(v) => updateRAG("embeddingModel", v)}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="text-embedding-3-small">text-embedding-3-small</SelectItem>
                    <SelectItem value="text-embedding-3-large">text-embedding-3-large</SelectItem>
                    <SelectItem value="text-embedding-ada-002">text-embedding-ada-002</SelectItem>
                    <SelectItem value="bge-large-en">BGE Large EN</SelectItem>
                    <SelectItem value="e5-large-v2">E5 Large v2</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="flex items-center justify-between">
                <Label>Use Reranker</Label>
                <Switch
                  checked={config.rag.useReranker}
                  onCheckedChange={(v) => updateRAG("useReranker", v)}
                />
              </div>
            </TabsContent>
          </div>
        </ScrollArea>
      </Tabs>
    </div>
  );
}

interface SliderFieldProps {
  label: string;
  value: number;
  min: number;
  max: number;
  step: number;
  onChange: (value: number) => void;
}

function SliderField({ label, value, min, max, step, onChange }: SliderFieldProps) {
  return (
    <div className="space-y-2">
      <div className="flex justify-between">
        <Label>{label}</Label>
        <span className="text-xs text-muted-foreground tabular-nums">{value.toFixed(2)}</span>
      </div>
      <Slider
        value={[value]}
        min={min}
        max={max}
        step={step}
        onValueChange={([v]) => onChange(v)}
      />
    </div>
  );
}
