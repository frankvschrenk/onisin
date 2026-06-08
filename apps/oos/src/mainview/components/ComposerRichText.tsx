// ComposerRichText.tsx — Rich-text composer for the chat input.
//
// Drop-in replacement for the previous plain Mantine <Textarea>. Uses
// the same MDXEditor + plugin set as PipelineRunPanel and
// AskResultPanel so authoring and reading share one Markdown engine:
// what the user pastes from a formatted source (Word, a web page, a
// Notion page) keeps its structure as Markdown, the LLM receives
// structured text instead of a plain-text blob, and the same
// Markdown round-trips into the assistant bubble (which already
// renders via MarkdownView).
//
// Keyboard semantics match the previous Textarea so muscle memory
// carries over:
//
//   - Enter alone        → send (intercepted, not a newline)
//   - Shift+Enter        → newline
//   - Cmd/Ctrl + "+"/"=" → toggle the view picker (when opts.canPickView)
//
// The component is controlled in the same sense as the old Textarea:
// it accepts a `value` prop and emits the current Markdown via
// `onChange`. The host owns the string state so the view-hint
// resolver effect in Chat.tsx keeps firing on every keystroke as
// before. setMarkdown() is only called when the host needs to
// programmatically set the draft (drag-drop replace, clear on send),
// driven through a forwarded ref.
//
// Height: the editor fills whatever vertical space its parent gives
// it. The previous Textarea grew autosize-style; here the host (the
// chat column's Split.Pane) owns the height — drag the resizer above
// the composer to grow or shrink the authoring area. A small minimum
// keeps the toolbar from looking cramped if the user shrinks the
// pane to the lower bound.

import {
	forwardRef,
	useEffect,
	useImperativeHandle,
	useRef,
} from "react";
import {
	MDXEditor,
	BoldItalicUnderlineToggles,
	UndoRedo,
	BlockTypeSelect,
	CreateLink,
	InsertTable,
	ListsToggle,
	toolbarPlugin,
	headingsPlugin,
	listsPlugin,
	quotePlugin,
	thematicBreakPlugin,
	tablePlugin,
	markdownShortcutPlugin,
	linkPlugin,
	linkDialogPlugin,
	type MDXEditorMethods,
} from "@mdxeditor/editor";
import { Box } from "@mantine/core";

export interface ComposerRichTextHandle {
	/** Replace the entire draft. Used for drop-replace and clear-on-send. */
	setMarkdown(value: string): void;
	/** Move keyboard focus into the editor. */
	focus(): void;
}

interface Props {
	/** Current draft (host-owned). Initial value of the editor at mount. */
	value:    string;
	/** Fired on every Markdown change — host stores it in `value`. */
	onChange: (value: string) => void;
	/** Triggered when Enter is pressed without Shift. */
	onSend:   () => void;
	/**
	 * Cmd/Ctrl + "+" toggles the view picker. Optional because some
	 * chat modes have no picker. When omitted the shortcut falls
	 * through to the browser default (zoom).
	 */
	onTogglePicker?: () => void;
	/** Disable the editor (busy state, locked composer in events mode). */
	disabled?:      boolean;
	/** Placeholder text shown when the editor is empty. */
	placeholder?:   string;
}

export const ComposerRichText = forwardRef<ComposerRichTextHandle, Props>(
	function ComposerRichText(props, ref) {
		const { value, onChange, onSend, onTogglePicker, disabled, placeholder } = props;
		const editorRef = useRef<MDXEditorMethods>(null);
		const containerRef = useRef<HTMLDivElement>(null);

		useImperativeHandle(ref, () => ({
			setMarkdown(next) {
				editorRef.current?.setMarkdown(next);
			},
			focus() {
				editorRef.current?.focus();
			},
		}));

		// Keyboard interception sits on the container's capture phase so
		// it runs before MDXEditor's internal handlers. Without capture,
		// Enter would insert a paragraph break before our handler ever
		// sees it.
		useEffect(() => {
			const el = containerRef.current;
			if (!el) return;

			const handler = (e: KeyboardEvent) => {
				if (disabled) return;

				// View picker shortcut: Cmd/Ctrl + "+"/"=".
				const isPlus = e.key === "+" || e.key === "=" || e.key === "Plus";
				if (isPlus && (e.metaKey || e.ctrlKey) && onTogglePicker) {
					e.preventDefault();
					e.stopPropagation();
					onTogglePicker();
					return;
				}

				// Enter without Shift → send. Shift+Enter falls through to
				// the editor's default (newline / new paragraph). Cmd/Ctrl
				// + Enter is left to the editor too — some users hit it
				// out of habit, and the editor treats it as a hard break,
				// which is fine.
				if (e.key === "Enter" && !e.shiftKey && !e.metaKey && !e.ctrlKey) {
					e.preventDefault();
					e.stopPropagation();
					onSend();
				}
			};

			el.addEventListener("keydown", handler, true /* capture */);
			return () => el.removeEventListener("keydown", handler, true);
		}, [disabled, onSend, onTogglePicker]);

		return (
			<Box
				ref={containerRef}
				style={{
					width: "100%",
					height: "100%",
					minHeight: 120,
					display: "flex",
					flexDirection: "column",
					border: "1px solid var(--mantine-color-gray-4)",
					borderRadius: "var(--mantine-radius-sm)",
					background: disabled
						? "var(--mantine-color-gray-1)"
						: "var(--mantine-color-body)",
					opacity: disabled ? 0.6 : 1,
					overflow: "hidden",
				}}
				data-placeholder={placeholder}
			>
				<Box style={{ flex: 1, minHeight: 0, overflow: "auto" }}>
				<MDXEditor
					ref={editorRef}
					markdown={value}
					onChange={onChange}
					readOnly={disabled}
					placeholder={placeholder}
					plugins={[
						toolbarPlugin({
							toolbarContents: () => (
								<>
									<UndoRedo />
									<BlockTypeSelect />
									<BoldItalicUnderlineToggles />
									<ListsToggle />
									<CreateLink />
									<InsertTable />
								</>
							),
						}),
						headingsPlugin(),
						listsPlugin(),
						quotePlugin(),
						thematicBreakPlugin(),
						tablePlugin(),
						linkPlugin(),
						linkDialogPlugin(),
						markdownShortcutPlugin(),
					]}
				/>
				</Box>
			</Box>
		);
	},
);
