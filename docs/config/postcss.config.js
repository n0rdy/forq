const autoprefixer = require('autoprefixer');
// v8 of the plugin exports the factory itself (no named `purgeCSSPlugin` export).
const purgeCSSPlugin = require('@fullhuman/postcss-purgecss');

module.exports = {
    plugins: [
        autoprefixer(),
        purgeCSSPlugin({
            content: ['./hugo_stats.json'],
            extractors: [
                {
                    extractor: (content) => {
                        const els = JSON.parse(content).htmlElements;
                        return [...(els.tags || []), ...(els.classes || []), ...(els.ids || [])];
                    },
                    extensions: ['json']
                }
            ],
            dynamicAttributes: [
                'aria-expanded',
                'data-bs-popper',
                'data-bs-target',
                'data-bs-theme',
                'data-dark-mode',
                'data-global-alert',
                'data-pane', // tabs.js
                'data-popper-placement',
                'data-sizes',
                'data-toggle-tab', // tabs.js
                'id',
                'size',
                'type'
            ],
            safelist: [
                'active',
                'btn-clipboard', // clipboards.js
                'btn-copy', // clipboards.js
                'clipboard', // clipboards.js
                'copy', // clipboards.js
                'disabled',
                'hidden',
                'modal-backdrop', // search-modal.js
                'selected', // search-modal.js
                'show',
                'img-fluid',
                'blur-up',
                'lazyload',
                'lazyloaded',
                'alert-link',
                'container-fw ',
                'container-lg',
                'container-fluid',
                'offcanvas-backdrop',
                'figcaption',
                'dt',
                'dd',
                'showing',
                'hiding',
                'page-item',
                'page-link',
                'not-content'
            ]
        })
    ]
};
