// Selecting in the Library: ticking covers or rows brings up a bar that
// Modifies or Removes the ticked ones. Rows loaded later by scrolling count too.
(function () {
  var form = document.getElementById('library-selection');
  if (!form) return;
  var bar = form.querySelector('.selection-bar');
  var shown = bar.querySelector('[data-selected]');
  function update() {
    var n = form.querySelectorAll('input[name=id]:checked').length;
    bar.hidden = n === 0;
    shown.textContent = n;
  }
  form.addEventListener('change', function (ev) {
    if (ev.target.matches('[data-select-all]')) {
      form.querySelectorAll('input[name=id]').forEach(function (b) { b.checked = ev.target.checked; });
    }
    update();
  });
  bar.querySelector('[data-clear]').addEventListener('click', function () {
    form.querySelectorAll('input[type=checkbox]').forEach(function (b) { b.checked = false; });
    update();
  });
  document.body.addEventListener('htmx:afterSwap', update);
  update();
})();
