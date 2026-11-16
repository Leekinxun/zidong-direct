// Apply the browser's preference before the stylesheet and application render.
try {
  document.documentElement.dataset.theme = localStorage.getItem('zd_theme') === 'light' ? 'light' : 'dark';
} catch {
  document.documentElement.dataset.theme = 'dark';
}
document.documentElement.style.colorScheme = document.documentElement.dataset.theme;
