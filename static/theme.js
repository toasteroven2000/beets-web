(function () {
  var dark = matchMedia('(prefers-color-scheme: dark)');
  var choice = 'auto';
  try {
    var stored = localStorage.getItem('theme');
    if (stored === 'light' || stored === 'dark') choice = stored;
  } catch (e) {}

  function apply() {
    var theme = choice === 'auto' ? (dark.matches ? 'dark' : 'light') : choice;
    document.documentElement.setAttribute('data-bs-theme', theme);
    var buttons = document.querySelectorAll('[data-theme-choice]');
    for (var i = 0; i < buttons.length; i++) {
      var on = buttons[i].getAttribute('data-theme-choice') === choice;
      buttons[i].classList.toggle('active', on);
      buttons[i].setAttribute('aria-pressed', on ? 'true' : 'false');
    }
  }

  apply();
  dark.addEventListener('change', apply);
  document.addEventListener('DOMContentLoaded', function () {
    apply();
    document.addEventListener('click', function (event) {
      var button = event.target.closest('[data-theme-choice]');
      if (!button) return;
      choice = button.getAttribute('data-theme-choice');
      try {
        if (choice === 'auto') localStorage.removeItem('theme');
        else localStorage.setItem('theme', choice);
      } catch (e) {}
      apply();
    });
  });
})();
