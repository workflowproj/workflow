import { useEffect, useRef } from "react";

// Open layers (modals, drawers, popovers) in the order they opened. Escape closes only the
// topmost one, so a popover inside a dialog inside a drawer unwinds one level at a time.
const stack: { current: () => void }[] = [];

function onKeyDown(e: KeyboardEvent) {
  if (e.key !== "Escape" || stack.length === 0) return;
  e.preventDefault();
  e.stopPropagation();
  stack[stack.length - 1].current();
}

/** useEscapeLayer calls onClose when Escape is pressed while this layer is the topmost open one. */
export function useEscapeLayer(open: boolean, onClose: () => void) {
  const handler = useRef(onClose);
  handler.current = onClose;
  useEffect(() => {
    if (!open) return;
    const entry = { current: () => handler.current() };
    stack.push(entry);
    if (stack.length === 1) document.addEventListener("keydown", onKeyDown, true);
    return () => {
      stack.splice(stack.indexOf(entry), 1);
      if (stack.length === 0) document.removeEventListener("keydown", onKeyDown, true);
    };
  }, [open]);
}
