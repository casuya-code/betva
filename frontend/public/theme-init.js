/* Loaded as a blocking same-origin script in <head> so the saved theme is
   applied before first paint. Kept external to satisfy the strict CSP. */
(function () {
  var theme = 'light';
  try {
    var saved = localStorage.getItem('betva_theme');
    if (saved === 'dark' || saved === 'light') {
      theme = saved;
    } else if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
      theme = 'dark';
    }
  } catch (e) {
    /* Storage or matchMedia unavailable: keep the light default. */
  }
  document.documentElement.setAttribute('data-theme', theme);
  var meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute('content', theme === 'dark' ? '#141719' : '#f6f5f2');
})();
