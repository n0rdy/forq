// Theme management for the Forq admin UI.
// Served as a static file (instead of an inline <script>) so the CSP can drop
// 'unsafe-inline' for scripts.
(function () {
    const htmlRoot = document.getElementById('html-root');
    if (!htmlRoot) {
        return;
    }

    const themeToggle = document.getElementById('theme-toggle');
    const sunIcon = document.getElementById('theme-icon-sun');
    const moonIcon = document.getElementById('theme-icon-moon');

    // Get saved theme or default to light
    const savedTheme = localStorage.getItem('forq-theme') || 'light';

    // Apply theme on page load
    function applyTheme(theme) {
        htmlRoot.setAttribute('data-theme', theme);
        if (sunIcon && moonIcon) {
            if (theme === 'dark') {
                sunIcon.classList.add('hidden');
                moonIcon.classList.remove('hidden');
            } else {
                sunIcon.classList.remove('hidden');
                moonIcon.classList.add('hidden');
            }
        }
    }

    // Initialize theme
    applyTheme(savedTheme);

    // Handle theme toggle
    if (themeToggle) {
        themeToggle.addEventListener('click', function () {
            const currentTheme = htmlRoot.getAttribute('data-theme');
            const newTheme = currentTheme === 'light' ? 'dark' : 'light';

            applyTheme(newTheme);
            localStorage.setItem('forq-theme', newTheme);
        });
    }
})();
