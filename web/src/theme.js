// Theme: which of jh-core's two token sets is in force.
//
// The light theme lives on :root and the dark theme on .jh-theme-dark, so the
// whole mechanism is one class on <html>. Three settings, not two — "system"
// is the default and follows prefers-color-scheme live, so an operator who
// never touches the control gets the room in whatever their desktop is in.
// The choice is per browser, kept in localStorage; index.html applies it
// before first paint from the same key.

const KEY = 'box-theme';

export const THEMES = [
  { id: 'system', label: 'System', icon: 'desktop' },
  { id: 'light', label: 'Light', icon: 'lightbulb' },
  { id: 'dark', label: 'Dark', icon: 'contrast' },
];

const media = matchMedia('(prefers-color-scheme: dark)');

export function getTheme() {
  try {
    const t = localStorage.getItem(KEY);
    if (THEMES.some((x) => x.id === t)) return t;
  } catch { /* storage blocked: system it is */ }
  return 'system';
}

export function isDark(theme = getTheme()) {
  return theme === 'dark' || (theme === 'system' && media.matches);
}

/** applyTheme puts the class on <html> and tells the page. */
export function applyTheme(theme = getTheme()) {
  const dark = isDark(theme);
  document.documentElement.classList.toggle('jh-theme-dark', dark);
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
  window.dispatchEvent(new CustomEvent('box-theme', { detail: { theme, dark } }));
}

export function setTheme(theme) {
  try {
    if (theme === 'system') localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, theme);
  } catch { /* still applies for this page */ }
  applyTheme(theme);
}

// The desktop changed under a "system" reader; another tab changed the choice.
media.addEventListener('change', () => { if (getTheme() === 'system') applyTheme('system'); });
window.addEventListener('storage', (e) => { if (e.key === KEY) applyTheme(); });
