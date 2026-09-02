// Copies the static manifest.json into dist/ after the 4 separate Vite
// builds above run (each with its own outDir, none of them at the dist/
// root) — see the vite.config.*.ts files' own doc comments for why this
// isn't one single Vite build with a shared publicDir.
import { copyFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
mkdirSync(resolve(root, "dist"), { recursive: true });
copyFileSync(resolve(root, "public/manifest.json"), resolve(root, "dist/manifest.json"));
console.log("Copied manifest.json to dist/");
