// Keys for an Import prompt: A applies the best match, U uses the files as
// they are, S skips, / searches.
// Typing in a field, or with a modifier held, is left alone.
document.addEventListener('keydown', function (ev) {
  if (ev.ctrlKey || ev.metaKey || ev.altKey || ev.repeat) return;
  if (ev.target.closest('input, textarea, select, [contenteditable]')) return;
  var el = document.querySelector('[data-key="' + CSS.escape(ev.key.toLowerCase()) + '"]');
  // Only what is on screen: a number shouldn't apply a match you can't see.
  if (!el || !el.getClientRects().length) return;
  ev.preventDefault();
  el.click();
  if (ev.key === '/') {
    var field = document.querySelector('#import-search input:not([type=hidden])');
    if (field) setTimeout(function () { field.focus(); }, 250);
  }
});
