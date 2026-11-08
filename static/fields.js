// Filters a record's field list by name, and to popular fields only.
document.querySelectorAll('[data-field-form]').forEach(function (form) {
  var filter = form.querySelector('[data-filter]');
  var popular = form.querySelector('[data-popular-only]');
  var count = form.querySelector('[data-count]');
  var rows = form.querySelectorAll('tr[data-field]');
  function apply() {
    var q = filter.value.trim().toLowerCase(), shown = 0;
    rows.forEach(function (row) {
      var ok = (!q || row.dataset.field.indexOf(q) >= 0) && (!popular.checked || row.hasAttribute('data-popular'));
      row.hidden = !ok;
      if (ok) shown++;
    });
    count.textContent = shown + ' of ' + rows.length + ' fields';
  }
  filter.addEventListener('input', apply);
  popular.addEventListener('change', apply);
  // Reset clears the filter too; apply once the form has reset.
  form.addEventListener('reset', function () { setTimeout(apply, 0); });
  apply();
});
