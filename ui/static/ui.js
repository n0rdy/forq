// Message row expansion for the queue page. A static file because the CSP
// has no 'unsafe-inline': inline onclick handlers never ran, so details rows
// stayed hidden and clicks on DLQ action buttons also fetched the row's details.
(function () {
    document.addEventListener('click', function (e) {
        const closer = e.target.closest('[data-close-details]');
        if (closer) {
            const details = document.getElementById(closer.dataset.closeDetails);
            if (details) details.classList.add('hidden');
            return;
        }
        if (e.target.closest('[data-row-actions]')) return;

        const row = e.target.closest('tr[data-details-url]');
        if (!row) return;
        const details = document.getElementById(row.dataset.detailsTarget);
        if (!details) return;
        details.classList.toggle('hidden');
        if (!details.classList.contains('hidden')) {
            htmx.ajax('GET', row.dataset.detailsUrl, { target: details, swap: 'innerHTML' });
        }
    });
})();
