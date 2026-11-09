function toggleMenu() {
    const nav = document.getElementById('mobileNav');
    nav.classList.toggle('active');
}

document.addEventListener('DOMContentLoaded', async () => {
    const container = document.getElementById('changelog-container');
    
    if (!container) return;
    
    try {
        const response = await fetch('https://raw.githubusercontent.com/1Maniak/nexus/main/docs/CHANGELOG.md');
        const text = await response.text();
        
        const versionRegex = /## \[v[0-9]/g;
        const matches = [...text.matchAll(versionRegex)];
        
        if (matches.length > 0) {
            let endIndex;
            if (matches.length >= 3) {
                endIndex = matches[2].index;
            } else {
                endIndex = text.length;
            }
            
            const startIndex = matches[0].index;
            const latestVersions = text.substring(startIndex, endIndex);
            
            container.innerHTML = marked.parse(latestVersions);
        } else {
            container.innerHTML = '<p>История изменений пуста.</p>';
        }
    } catch (error) {
        console.error('Ошибка загрузки CHANGELOG:', error);
        container.innerHTML = '<p>Не удалось загрузить историю изменений.</p>';
    }
});