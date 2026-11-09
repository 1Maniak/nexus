document.addEventListener('DOMContentLoaded', async () => {
    const container = document.getElementById('changelog-container');
    
    try {
        const response = await fetch('https://raw.githubusercontent.com/1Maniak/nexus/main/docs/CHANGELOG.md');
        const text = await response.text();
        
        // Отрезаем блок [Unreleased], берем только первую версию
        const parts = text.split('## [');
        let latestVersion = parts[1];
        
        if (latestVersion) {
            // Возвращаем префикс "## [" для корректного парсинга
            latestVersion = '## [' + latestVersion;
            
            // Обрезаем до следующей версии
            const nextVersionIndex = latestVersion.indexOf('## [', 2);
            if (nextVersionIndex !== -1) {
                latestVersion = latestVersion.substring(0, nextVersionIndex);
            }
            
            // Парсим Markdown в HTML
            container.innerHTML = marked.parse(latestVersion);
        } else {
            container.innerHTML = '<p>История изменений пуста.</p>';
        }
    } catch (error) {
        console.error('Ошибка загрузки CHANGELOG:', error);
        container.innerHTML = '<p>Не удалось загрузить историю изменений. Пожалуйста, посетите <a href="https://github.com/1Maniak/nexus/releases">GitHub Releases</a>.</p>';
    }
});