// Applies the saved theme before the app renders, so the page never flashes the wrong colours.
// Kept in sync with src/lib/theme.ts. A separate file because the CSP forbids inline scripts.
(function () {
  var pref = "system";
  try {
    pref = localStorage.getItem("wfm.theme") || "system";
  } catch (e) {}
  var dark = pref === "dark" || (pref !== "light" && window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
})();
