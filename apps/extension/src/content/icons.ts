// A hand-inlined copy of lucide-react's own "X" icon path (the same one
// apps/web's Dialog close button uses, via lucide's XIcon) — pulling in
// the actual lucide-react package just for one glyph isn't worth a
// dependency in a bundle this size, but the markup should still look like
// the same icon, not a generic "×" text glyph in whatever font the host
// page happens to render this in.
export const CLOSE_ICON_SVG = `<svg xmlns="http://www.w3.org/2000/svg" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>`;

// Same provenance as CLOSE_ICON_SVG above (a hand-inlined lucide-react
// "send" path) — the reply-send button in the comment thread UI is
// icon-only.
export const SEND_ICON_SVG = `<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14.536 21.686a.5.5 0 0 0 .937-.024l6.5-19a.496.496 0 0 0-.635-.635l-19 6.5a.5.5 0 0 0-.024.937l7.93 3.18a2 2 0 0 1 1.112 1.11z"/><path d="m21.854 2.147-10.94 10.939"/></svg>`;
