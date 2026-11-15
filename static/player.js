// One player for the page. A button with data-play="URL" plays that file;
// pressing it again pauses; when a file ends, the next button on the page plays.
(function () {
  var bar = document.getElementById('player');
  if (!bar) return;
  var audio = bar.querySelector('audio');
  var title = bar.querySelector('.player-title');
  var current = null;

  function buttons() { return Array.prototype.slice.call(document.querySelectorAll('[data-play]')); }

  function show() {
    buttons().forEach(function (b) {
      var on = b === current && !audio.paused;
      b.classList.toggle('playing', on);
      var icon = b.querySelector('i');
      if (icon) icon.className = 'bi ' + (on ? 'bi-pause-fill' : 'bi-play-fill');
    });
  }

  function play(button) {
    if (button === current && audio.src) {
      if (audio.paused) audio.play(); else audio.pause();
      return;
    }
    current = button;
    audio.src = button.getAttribute('data-play');
    title.textContent = button.getAttribute('data-title') || '';
    title.title = title.textContent;
    bar.hidden = false;
    document.body.classList.add('has-player');
    audio.play();
    show();
  }

  document.addEventListener('click', function (e) {
    var button = e.target.closest('[data-play]');
    if (button) { e.preventDefault(); play(button); return; }
    if (e.target.closest('[data-player-close]')) {
      audio.pause();
      audio.removeAttribute('src');
      audio.load();
      current = null;
      bar.hidden = true;
      document.body.classList.remove('has-player');
      show();
    }
  });

  audio.addEventListener('play', show);
  audio.addEventListener('pause', show);
  audio.addEventListener('ended', function () {
    var all = buttons();
    var next = all[all.indexOf(current) + 1];
    if (next) play(next); else show();
  });
  audio.addEventListener('error', function () {
    title.textContent = "Can't play " + (title.textContent || 'this file');
  });
})();
