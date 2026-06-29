// Module-level tracker for the most recently double-click-toggled task.
// Used by TaskCard and TaskRow to play an enter animation when the
// card reappears in its new column after the API update.

let lastToggledId: string | null = null;
let lastToggledAt = 0;

export function markToggled(taskId: string) {
  lastToggledId = taskId;
  lastToggledAt = Date.now();
}

export function consumeToggled(taskId: string): boolean {
  if (lastToggledId === taskId && Date.now() - lastToggledAt < 2000) {
    lastToggledId = null;
    return true;
  }
  return false;
}
