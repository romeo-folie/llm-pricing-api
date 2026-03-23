export interface UseCase {
  slug: string
  label: string
  icon: string
  description: string
}

export const USE_CASES: UseCase[] = [
  { slug: "coding", label: "Coding", icon: "⌨️", description: "Code generation, debugging, and software engineering tasks." },
  { slug: "reasoning", label: "Reasoning", icon: "🧮", description: "Math, logic puzzles, and complex multi-step reasoning." },
  { slug: "finance", label: "Finance", icon: "📊", description: "Financial analysis, CFA-level knowledge, and investment research." },
  { slug: "writing", label: "Writing", icon: "✍️", description: "Copywriting, content creation, and instruction following." },
  { slug: "video", label: "Video", icon: "🎬", description: "Video understanding, transcription, and multimodal analysis." },
  { slug: "agentic", label: "Agentic", icon: "🤖", description: "Autonomous agents, tool use, and function calling workflows." },
  { slug: "general", label: "General", icon: "💬", description: "General-purpose chat, Q&A, and everyday tasks." },
]

export const USE_CASE_SLUGS = USE_CASES.map((uc) => uc.slug)
