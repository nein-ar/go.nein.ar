// godoc page behaviour: syntax highlighting, click to copy, the symbol
// index in the sidebar and the 404 terminal. every hook is a no-op when
// its element is missing, so one script serves every layout
const FLASH_MS = 1500;
const SCROLL_OFFSET = 120;

function highlight() {
    if (window.hljs) hljs.highlightAll();
}

function bindCopy() {
    document.querySelectorAll('[data-copy]').forEach(el => {
        el.addEventListener('click', () => {
            if (navigator.clipboard) navigator.clipboard.writeText(el.dataset.copy);
            el.classList.add('copied');
            setTimeout(() => el.classList.remove('copied'), FLASH_MS);
        });
    });
}

function symbolKind(a) {
    if (a.closest('.godoc-sym-method')) return 'method';
    if (a.closest('.godoc-sym-constructor')) return 'ctor';
    return 'fn';
}

function tocRow(kind, text, id) {
    const row = document.createElement('div');
    row.className = 'toc-row toc-' + kind;
    row.dataset.text = text.toLowerCase();

    const link = document.createElement('a');
    link.href = '#' + id;
    link.textContent = text;
    row.appendChild(link);
    return row;
}

// a section with no symbol headings (constants, variables) has nothing to list
function buildToc(toc) {
    document.querySelectorAll('.godoc-section').forEach(sec => {
        const title = sec.querySelector('.godoc-section-title a');
        const syms = sec.querySelectorAll('.godoc-sym-title a');
        if (!title || syms.length === 0) return;

        toc.appendChild(tocRow('sec', title.textContent, sec.id));
        syms.forEach(a => {
            const id = a.closest('[id]').id;
            const row = tocRow(symbolKind(a), a.textContent.trim(), id);
            row.querySelector('a').addEventListener('click', () => flash(id));
            toc.appendChild(row);
        });
    });
}

function flash(id) {
    const target = document.getElementById(id);
    if (!target) return;
    target.classList.add('godoc-highlight');
    setTimeout(() => target.classList.remove('godoc-highlight'), FLASH_MS);
}

function bindTocFilter(toc) {
    const button = document.getElementById('pkg-toc-filter-btn');
    const wrap = document.getElementById('pkg-toc-search-wrap');
    const input = document.getElementById('pkg-toc-search');

    const apply = query => {
        const q = query.toLowerCase();
        toc.querySelectorAll('.toc-row').forEach(row => {
            row.hidden = q !== '' && !row.dataset.text.includes(q);
        });
    };

    button.addEventListener('click', () => {
        wrap.hidden = !wrap.hidden;
        if (wrap.hidden) {
            input.value = '';
            apply('');
        } else {
            input.focus();
        }
    });
    input.addEventListener('input', () => apply(input.value));
}

// highlights the symbol whose block was last scrolled past, and keeps its
// row inside the index viewport so long indexes follow the reader
function bindScrollSpy(toc) {
    const blocks = document.querySelectorAll('.godoc-decl[id], .godoc-type-block[id], .godoc-section[id]');
    const links = toc.querySelectorAll('a');

    const update = () => {
        const y = window.scrollY + SCROLL_OFFSET;
        let active = null;
        blocks.forEach(el => { if (el.offsetTop <= y) active = el.id; });

        links.forEach(a => {
            const on = a.getAttribute('href') === '#' + active;
            a.classList.toggle('active', on);
            if (!on) return;
            const row = a.parentElement;
            if (row.offsetTop < toc.scrollTop || row.offsetTop > toc.scrollTop + toc.clientHeight) {
                toc.scrollTop = row.offsetTop - toc.clientHeight / 2;
            }
        });
    };

    window.addEventListener('scroll', update, { passive: true });
    update();
}

function bindToc() {
    const toc = document.getElementById('pkg-toc');
    const widget = document.getElementById('pkg-toc-widget');
    if (!toc || !widget) return;

    buildToc(toc);
    if (toc.children.length === 0) return;

    widget.hidden = false;
    bindTocFilter(toc);
    bindScrollSpy(toc);
}

// the page is served for any missing path, so the terminal fills in the
// real one at load time
function fillNotFound() {
    const url = document.getElementById('log-url');
    const cmd = document.getElementById('cmd-url');
    if (cmd) cmd.textContent = window.location.pathname;
    if (url) url.textContent = window.location.href;

    const now = new Date().toISOString().replace('T', ' ').split('.')[0];
    document.querySelectorAll('.curr-time').forEach(el => { el.textContent = now; });
}

document.addEventListener('DOMContentLoaded', () => {
    highlight();
    bindCopy();
    bindToc();
    fillNotFound();
});
