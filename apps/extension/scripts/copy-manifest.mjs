// Copies the static manifest.json (and the icons/ it references) into
// dist/ after the 4 separate Vite builds above run (each with its own
// outDir, none of them at the dist/ root) — see the vite.config.*.ts
// files' own doc comments for why this isn't one single Vite build with a
// shared publicDir.
import { copyFileSync, cpSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
mkdirSync(resolve(root, "dist"), { recursive: true });
copyFileSync(resolve(root, "public/manifest.json"), resolve(root, "dist/manifest.json"));
console.log("Copied manifest.json to dist/");

// icon16/32/48/128.png (referenced by manifest.json's icons/action.default_icon)
// — paca-logo-source.png is the original asset they're generated from, kept
// alongside them for future re-exports, not itself referenced by the
// manifest, so it doesn't need to ship, but copying the whole directory is
// simpler than filtering it out.
cpSync(resolve(root, "public/icons"), resolve(root, "dist/icons"), { recursive: true });
console.log("Copied icons/ to dist/");
