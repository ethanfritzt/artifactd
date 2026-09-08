(() => {
  const slides = [...document.querySelectorAll('.slide')];
  const progress = document.querySelector('#progress');
  const count = document.querySelector('#slide-count');
  const previous = document.querySelector('#prev');
  const next = document.querySelector('#next');
  const fullscreen = document.querySelector('#fullscreen');
  let current = Math.max(0, Math.min(slides.length - 1, Number(location.hash.slice(1)) - 1 || 0));

  slides.forEach((slide, index) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.setAttribute('role', 'tab');
    button.setAttribute('aria-label', `Go to slide ${index + 1}: ${slide.dataset.title}`);
    button.addEventListener('click', () => show(index));
    progress.append(button);
  });

  function show(index, updateHash = true) {
    current = Math.max(0, Math.min(slides.length - 1, index));
    slides.forEach((slide, slideIndex) => {
      const active = slideIndex === current;
      slide.hidden = !active;
      slide.setAttribute('aria-hidden', String(!active));
    });
    [...progress.children].forEach((button, index) => {
      button.setAttribute('aria-selected', String(index === current));
      button.tabIndex = index === current ? 0 : -1;
    });
    count.textContent = `${String(current + 1).padStart(2, '0')} / ${String(slides.length).padStart(2, '0')}`;
    previous.disabled = current === 0;
    next.disabled = current === slides.length - 1;
    if (updateHash) history.replaceState(null, '', `#${current + 1}`);
  }

  function move(delta) { show(current + delta); }

  previous.addEventListener('click', () => move(-1));
  next.addEventListener('click', () => move(1));
  fullscreen.addEventListener('click', async () => {
    if (!document.fullscreenElement) await document.documentElement.requestFullscreen?.();
    else await document.exitFullscreen?.();
  });
  window.addEventListener('hashchange', () => show(Number(location.hash.slice(1)) - 1, false));
  document.addEventListener('keydown', (event) => {
    if (['INPUT', 'TEXTAREA', 'SELECT'].includes(event.target.tagName)) return;
    if (['ArrowRight', 'PageDown', ' '].includes(event.key)) { event.preventDefault(); move(1); }
    if (['ArrowLeft', 'PageUp'].includes(event.key)) { event.preventDefault(); move(-1); }
    if (event.key === 'Home') { event.preventDefault(); show(0); }
    if (event.key === 'End') { event.preventDefault(); show(slides.length - 1); }
    if (event.key.toLowerCase() === 'f') fullscreen.click();
  });
  show(current, false);
})();
