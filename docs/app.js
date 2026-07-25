/* SIGINT front end.
   Everything is client side: the Go binary writes edition.json, this draws it.
   Filtering never hits the network because the whole edition is already here. */

'use strict';

const POLL_MS = 5 * 60 * 1000;

/* Display order, strongest first. Must match config.Quadrants in Go.
   Position is the cell each one occupies in the 2x2 matrix glyph:
       novelty high ->  NEW GROUND | BREAK GLASS
       novelty low  ->  NOISE FLOOR| STANDING ORDERS
                        act low    | act high        */
const QUADRANTS = [
  { key: 'BREAK GLASS',     cell: 1, note: 'new and urgent' },
  { key: 'STANDING ORDERS', cell: 3, note: 'nothing new, act anyway' },
  { key: 'NEW GROUND',      cell: 0, note: 'new, nothing to do yet' },
  { key: 'NOISE FLOOR',     cell: 2, note: 'logged, not urgent' }
];

const QUAD_KEYS = QUADRANTS.map(q => q.key);

let edition = null;
let sourceFilter = 'all';
let quadFilter = new Set(QUAD_KEYS);

/* ------------------------------------------------------------------ loading */

async function load() {
  try {
    const res = await fetch('edition.json?t=' + Date.now(), { cache: 'no-store' });
    if (!res.ok) throw new Error(res.status + ' ' + res.statusText);
    edition = await res.json();
    renderChrome();
    renderStories();
    renderNoise();
  } catch (err) {
    if (edition) return;            // keep showing the last good edition
    document.getElementById('stories').innerHTML =
      '<p class="state">No edition available yet. ' +
      'Run <code>go run ./cmd/edition</code> to build one.</p>';
  }
}

/* ---------------------------------------------------------------- rendering */

function renderChrome() {
  document.getElementById('standfirst').textContent = edition.standfirst;
  document.getElementById('colophon-text').textContent = edition.colophon;

  const date = new Date(edition.generated);
  document.getElementById('edition-line').textContent =
    'No. ' + edition.edition + ' — ' +
    date.toLocaleDateString(undefined, {
      weekday: 'long', year: 'numeric', month: 'long', day: 'numeric'
    });

  document.getElementById('refreshed').textContent =
    'Last refreshed ' + date.toLocaleTimeString(undefined,
      { hour: '2-digit', minute: '2-digit' }) + '.';

  buildSourceChips();
  buildQuadrantChips();
}

function buildSourceChips() {
  const box = document.getElementById('source-filters');
  const counts = edition.counts.by_source || {};
  const sources = ['all'].concat(Object.keys(counts).sort());

  box.innerHTML = '';
  sources.forEach(src => {
    const n = src === 'all' ? edition.counts.total : counts[src];
    box.appendChild(makeChip(labelFor(src), n, sourceFilter === src, null, () => {
      sourceFilter = src;
      redraw();
    }));
  });
}

function buildQuadrantChips() {
  const box = document.getElementById('quadrant-filters');
  const counts = edition.counts.by_quadrant || {};

  box.innerHTML = '';

  const allOn = quadFilter.size === QUAD_KEYS.length;
  box.appendChild(makeChip('All', edition.counts.total, allOn, null, () => {
    quadFilter = new Set(QUAD_KEYS);
    redraw();
  }));

  QUADRANTS.forEach(q => {
    const on = quadFilter.size !== QUAD_KEYS.length && quadFilter.has(q.key);
    box.appendChild(makeChip(q.key, counts[q.key] || 0, on, q.note, () => {
      // Clicking a quadrant isolates it; clicking again restores the rest.
      quadFilter = (quadFilter.size === 1 && quadFilter.has(q.key))
        ? new Set(QUAD_KEYS)
        : new Set([q.key]);
      redraw();
    }));
  });
}

function makeChip(text, count, pressed, title, onClick) {
  const b = document.createElement('button');
  b.className = 'chip';
  b.type = 'button';
  if (title) b.title = title;
  b.setAttribute('aria-pressed', String(pressed));
  b.innerHTML = escapeHTML(text) +
    '<span class="chip__count">' + (count || 0) + '</span>';
  b.addEventListener('click', onClick);
  return b;
}

function redraw() {
  renderChrome();
  renderStories();
}

function renderStories() {
  const visible = edition.items.filter(item =>
    (sourceFilter === 'all' || item.source === sourceFilter) &&
    quadFilter.has(item.quadrant)
  );

  const box = document.getElementById('stories');

  if (!visible.length) {
    box.innerHTML = '<p class="state">Nothing in this quadrant right now. ' +
      'Widen the filter, or come back after the next refresh.</p>';
    return;
  }

  box.innerHTML = visible.map((item, i) => storyHTML(item, i === 0)).join('');
}

function renderNoise() {
  const box = document.getElementById('noise');
  const items = edition.noise || [];

  if (!items.length) {
    box.innerHTML = '';
    return;
  }

  box.innerHTML =
    '<h2 class="noise__head">Filed under noise</h2>' +
    '<p class="noise__intro">Ran hot elsewhere today. Scored low here.</p>' +
    '<ul class="noise__list">' +
    items.map(item => `
      <li class="noise__item">
        <a href="${escapeAttr(item.url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(item.title)}</a>
        <span class="noise__meta">${item.points} points · scored ${item.novelty}/${item.actionability}</span>
      </li>`).join('') +
    '</ul>';
}

function storyHTML(item, isLead) {
  const quad = QUADRANTS.find(q => q.key === item.quadrant) || QUADRANTS[3];
  const slug = quad.key.toLowerCase().replace(/ /g, '-');

  return `
  <article class="story${isLead ? ' story--lead' : ''}">
    <div class="rail" aria-label="Novelty ${item.novelty} of 5, actionability ${item.actionability} of 5, ${escapeHTML(item.quadrant)}">
      <div class="rail__axis"><span>NOV</span><span class="rail__val">${item.novelty}</span></div>
      ${barHTML(item.novelty)}
      <div class="rail__axis"><span>ACT</span><span class="rail__val">${item.actionability}</span></div>
      ${barHTML(item.actionability)}
      ${matrixHTML(quad.cell, slug)}
      <div class="rail__quad rail__quad--${slug}">${escapeHTML(item.quadrant)}</div>
    </div>

    <div class="story__body">
      <h2 class="story__title">
        <a href="${escapeAttr(item.url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(item.title)}</a>
      </h2>
      ${item.dek ? `<p class="story__dek">${escapeHTML(item.dek)}</p>` : ''}
      ${item.unproven ? `<p class="story__unproven"><span>Not established:</span> ${escapeHTML(item.unproven)}</p>` : ''}
      ${priorHTML(item)}
      <p class="story__meta">
        <em>${labelFor(item.source)}</em>
        ${item.authors ? ' · ' + escapeHTML(item.authors) : ''}
        ${item.points ? ' · ' + item.points + ' points' : ''}
        · ${relativeTime(item.published)}
      </p>
    </div>
  </article>`;
}

/* The signature element: a literal 2x2 with the live cell filled.
   Reading order is top-left, top-right, bottom-left, bottom-right. */
function matrixHTML(activeCell, slug) {
  let cells = '';
  for (let n = 0; n < 4; n++) {
    cells += `<span class="matrix__cell${n === activeCell ? ' matrix__cell--on matrix__cell--' + slug : ''}"></span>`;
  }
  return `<div class="matrix" aria-hidden="true">${cells}</div>`;
}

/* The memory no other aggregator has: this CVE was on our page before it was
   confirmed exploited. */
function priorHTML(item) {
  if (!item.prior_title) return '';
  const when = item.prior_days <= 0 ? 'earlier today'
    : item.prior_days === 1 ? 'yesterday'
    : item.prior_days + ' days earlier';
  return `<p class="story__prior">
    <span>We ran this ${when}:</span>
    <a href="${escapeAttr(item.prior_url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(item.prior_title)}</a>
  </p>`;
}

function barHTML(value) {
  let ticks = '';
  for (let n = 1; n <= 5; n++) {
    ticks += `<span class="rail__tick${n <= value ? ' rail__tick--on' : ''}"></span>`;
  }
  return `<div class="rail__bar" aria-hidden="true">${ticks}</div>`;
}

/* ------------------------------------------------------------------ helpers */

function labelFor(source) {
  return { all: 'All', arxiv: 'arXiv', hn: 'Hacker News', kev: 'CISA KEV' }[source] || source;
}

function relativeTime(iso) {
  const mins = Math.round((Date.now() - new Date(iso)) / 60000);
  if (mins < 60) return mins + 'm ago';
  const hrs = Math.round(mins / 60);
  if (hrs < 48) return hrs + 'h ago';
  return Math.round(hrs / 24) + 'd ago';
}

function escapeHTML(s) {
  return String(s === undefined || s === null ? '' : s).replace(
    /[&<>"']/g,
    c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])
  );
}

const escapeAttr = escapeHTML;

/* --------------------------------------------------------------------- boot */

load();
setInterval(load, POLL_MS);
document.addEventListener('visibilitychange', () => {
  if (!document.hidden) load();     // catch up the moment you switch back
});
