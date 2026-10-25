import { useEffect } from "react";
import { EditorContent, useEditor, useEditorState } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Placeholder from "@tiptap/extension-placeholder";
import { Markdown as MarkdownExt } from "@tiptap/markdown";
import { Bold, Code, Heading2, Italic, Link2, List, ListOrdered, Quote, SquareCode, Strikethrough } from "lucide-react";
import { cn } from "@/lib/cn";

/**
 * MarkdownEditor is a Tiptap rich-text editor whose value is Markdown, the format the API
 * stores for descriptions and comments. Cmd/Ctrl+Enter calls onSubmit.
 */
export function MarkdownEditor({
  value,
  onChange,
  placeholder,
  autoFocus,
  onSubmit,
  minHeight = 96,
  className,
}: {
  value: string;
  onChange: (md: string) => void;
  placeholder?: string;
  autoFocus?: boolean;
  onSubmit?: () => void;
  minHeight?: number;
  className?: string;
}) {
  const editor = useEditor({
    extensions: [
      StarterKit.configure({ link: { openOnClick: false, autolink: true } }),
      Placeholder.configure({ placeholder: placeholder ?? "Write something…" }),
      MarkdownExt,
    ],
    content: value,
    contentType: "markdown",
    autofocus: autoFocus ? "end" : false,
    editorProps: {
      attributes: { class: "prose-wfm text-sm px-3 py-2.5 tiptap", style: `min-height:${minHeight}px` },
      handleKeyDown: (_view, e) => {
        if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && onSubmit) {
          onSubmit();
          return true;
        }
        return false;
      },
    },
    onUpdate: ({ editor }) => onChange(editor.getMarkdown()),
  });

  // External resets (e.g. clearing after a comment is posted).
  useEffect(() => {
    if (editor && value === "" && !editor.isEmpty) editor.commands.clearContent();
  }, [editor, value]);

  const state = useEditorState({
    editor,
    selector: ({ editor: e }) =>
      e
        ? {
            bold: e.isActive("bold"),
            italic: e.isActive("italic"),
            strike: e.isActive("strike"),
            code: e.isActive("code"),
            h2: e.isActive("heading", { level: 2 }),
            bullet: e.isActive("bulletList"),
            ordered: e.isActive("orderedList"),
            quote: e.isActive("blockquote"),
            codeBlock: e.isActive("codeBlock"),
            link: e.isActive("link"),
          }
        : null,
  });
  if (!editor) return null;
  const chain = () => editor.chain().focus();
  const tools = [
    { label: "Bold", icon: Bold, active: state?.bold, run: () => chain().toggleBold().run() },
    { label: "Italic", icon: Italic, active: state?.italic, run: () => chain().toggleItalic().run() },
    { label: "Strikethrough", icon: Strikethrough, active: state?.strike, run: () => chain().toggleStrike().run() },
    { label: "Inline code", icon: Code, active: state?.code, run: () => chain().toggleCode().run() },
    { label: "Heading", icon: Heading2, active: state?.h2, run: () => chain().toggleHeading({ level: 2 }).run() },
    { label: "Bulleted list", icon: List, active: state?.bullet, run: () => chain().toggleBulletList().run() },
    { label: "Numbered list", icon: ListOrdered, active: state?.ordered, run: () => chain().toggleOrderedList().run() },
    { label: "Quote", icon: Quote, active: state?.quote, run: () => chain().toggleBlockquote().run() },
    { label: "Code block", icon: SquareCode, active: state?.codeBlock, run: () => chain().toggleCodeBlock().run() },
    {
      label: "Link",
      icon: Link2,
      active: state?.link,
      run: () => {
        const prev = editor.getAttributes("link").href as string | undefined;
        const url = window.prompt("Link URL", prev ?? "https://");
        if (url === null) return;
        if (url === "") chain().extendMarkRange("link").unsetLink().run();
        else chain().extendMarkRange("link").setLink({ href: url }).run();
      },
    },
  ];
  return (
    <div className={cn("rounded-md border border-line-strong bg-surface focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/25", className)}>
      <div className="flex flex-wrap gap-0.5 border-b border-line px-1.5 py-1">
        {tools.map((t) => (
          <button
            key={t.label}
            type="button"
            title={t.label}
            aria-label={t.label}
            aria-pressed={!!t.active}
            onMouseDown={(e) => e.preventDefault()}
            onClick={t.run}
            className={cn("inline-flex size-7 items-center justify-center rounded text-muted hover:bg-surface-2 hover:text-ink", t.active && "bg-surface-3 text-ink")}
          >
            <t.icon className="size-3.5" />
          </button>
        ))}
      </div>
      <EditorContent editor={editor} />
    </div>
  );
}
