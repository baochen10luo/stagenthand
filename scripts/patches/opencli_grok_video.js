import { cli, Strategy } from '@jackwener/opencli/registry';
import { writeFileSync, mkdirSync, createWriteStream, unlinkSync } from 'fs';
import { join } from 'path';
import { tmpdir } from 'os';
import { get as httpsGet } from 'https';
import { get as httpGet } from 'http';

function downloadUrl(url, destPath) {
    return new Promise((resolve, reject) => {
        const getter = url.startsWith('https') ? httpsGet : httpGet;
        const file = createWriteStream(destPath);
        getter(url, (res) => {
            if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
                file.close();
                try { unlinkSync(destPath); } catch(e) {}
                return downloadUrl(res.headers.location, destPath).then(resolve).catch(reject);
            }
            if (res.statusCode !== 200) {
                file.close();
                return reject(new Error('HTTP ' + res.statusCode));
            }
            res.pipe(file);
            file.on('finish', () => file.close(resolve));
        }).on('error', (e) => { file.close(); reject(e); });
    });
}

const IMAGINE_URL = 'https://grok.com/imagine';

const BLOB_INTERCEPTOR = `
(function() {
    if (window.__blobIntercepted) return;
    window.__blobIntercepted = true;
    window.__downloadData = [];
    const origBlobUrl = URL.createObjectURL;
    URL.createObjectURL = function(blob) {
        const url = origBlobUrl.call(URL, blob);
        const reader = new FileReader();
        reader.onloadend = function() {
            window.__downloadData.push({ url, type: blob.type, size: blob.size, b64: reader.result });
        };
        reader.readAsDataURL(blob);
        return url;
    };
})()
`;

export const videoCommand = cli({
    site: 'grok',
    name: 'video',
    description: 'Generate videos using Grok Imagine',
    domain: 'grok.com',
    strategy: Strategy.COOKIE,
    browser: true,
    args: [
        { name: 'prompt', positional: true, type: 'string', required: true, help: 'Video generation prompt' },
        { name: 'image', type: 'string', default: '', help: 'Path to reference image for image-to-video generation' },
        { name: 'output', type: 'string', default: '', help: 'Output directory (default: ~/Downloads/grok-videos)' },
        { name: 'timeout', type: 'int', default: 180, help: 'Max seconds to wait for generation (default: 180)' },
    ],
    columns: ['file'],
    func: async (page, kwargs) => {
        const prompt = kwargs.prompt;
        const timeoutMs = (kwargs.timeout || 180) * 1000;
        const outputDir = kwargs.output || join(process.env.HOME || tmpdir(), 'Downloads', 'grok-videos');

        // Navigate to Grok Imagine
        await page.goto(IMAGINE_URL);
        await page.wait(6);

        // Dismiss cookie consent dialog if present — retry up to 3 times
        for (let d = 0; d < 3; d++) {
            const dismissed = await page.evaluate(`
                (function() {
                    const closeBtn = document.querySelector('#close-pc-btn-handler') ||
                        document.querySelector('button[aria-label="关闭"]') ||
                        document.querySelector('button[aria-label="關閉"]');
                    if (closeBtn) { closeBtn.click(); return true; }
                    const allow = Array.from(document.querySelectorAll('button')).find(b =>
                        (b.textContent || '').includes('全部允許') || (b.id || '').includes('accept-recommended')
                    );
                    if (allow) { allow.click(); return true; }
                    // No dialog found
                    return !document.querySelector('[role=dialog]');
                })()
            `);
            if (dismissed) break;
            await page.wait(2);
        }
        await page.wait(2);

        // Install blob interceptor early
        await page.evaluate(BLOB_INTERCEPTOR);

        // Switch to video mode — retry up to 10 times with increasing wait
        let switched = false;
        for (let attempt = 0; attempt < 10; attempt++) {
            switched = await page.evaluate(`
                (function() {
                    const btns = Array.from(document.querySelectorAll('button[role=radio]'));
                    const videoBtn = btns.find(b => {
                        const span = b.querySelector('span');
                        return (b.textContent || '').includes('影片') || (span && span.textContent.includes('影片'));
                    });
                    if (!videoBtn) return false;
                    videoBtn.click();
                    return true;
                })()
            `);
            if (switched) break;
            await page.wait(attempt < 3 ? 2 : 4);
        }

        if (!switched) return [{ file: '[ERROR] video mode button not found' }];

        await page.wait(1);

        // Upload reference image if provided (image-to-video)
        if (kwargs.image) {
            const { existsSync } = await import('fs');
            if (!existsSync(kwargs.image)) {
                return [{ file: `[ERROR] image file not found: ${kwargs.image}` }];
            }
            // Click the 圖片 button to reveal/activate the file input
            await page.evaluate(`
                (function() {
                    const btn = Array.from(document.querySelectorAll('button')).find(b =>
                        (b.textContent || '').trim() === '圖片' ||
                        (b.getAttribute('aria-label') || '').includes('圖片')
                    );
                    if (btn) btn.click();
                })()
            `);
            await page.wait(1);
            // Set file on the hidden input — signature: setFileInput(files[], selector)
            try {
                await page.setFileInput([kwargs.image], 'input[type=file][accept="image/*"]');
            } catch (e) {
                await page.setFileInput([kwargs.image], 'input[type=file]');
            }
            await page.wait(2);
        }

        // Type prompt using CDP Input.insertText (ProseMirror compatible)
        let typed = false;
        for (let attempt = 0; attempt < 5; attempt++) {
            const editorExists = await page.evaluate(`!!document.querySelector('[contenteditable="true"]')`);
            if (editorExists) {
                try {
                    await page.evaluate(`document.querySelector('[contenteditable="true"]').focus()`);
                    await page.wait(0.3);
                    await page.insertText(prompt);
                    const content = await page.evaluate(`document.querySelector('[contenteditable="true"]')?.textContent || ''`);
                    if (content.trim()) { typed = true; break; }
                } catch (e) { /* retry */ }
            }
            await page.wait(2);
        }

        if (!typed) return [{ file: '[ERROR] input field not found' }];

        await page.wait(1);

        // Submit via 送出 button
        const submitted = await page.evaluate(`
            (function() {
                const btn = document.querySelector('button[aria-label="送出"]') ||
                            Array.from(document.querySelectorAll('button[type=submit]')).find(b => !b.disabled);
                if (!btn || btn.disabled) return false;
                btn.click();
                return true;
            })()
        `);

        if (!submitted) return [{ file: '[ERROR] submit button not found or disabled' }];

        // Wait for navigation to /imagine/post/UUID
        const startTime = Date.now();
        let onPostPage = false;

        while (Date.now() - startTime < 30000) {
            await page.wait(2);
            const href = await page.evaluate(`window.location.href`);
            if (typeof href === 'string' && href.includes('/imagine/post/')) {
                onPostPage = true;
                break;
            }
        }

        if (!onPostPage) return [{ file: '[ERROR] page did not navigate to post URL' }];

        // Re-install blob interceptor after navigation
        await page.evaluate(BLOB_INTERCEPTOR);

        // I2V two-step: if '製作影片' button exists, click it to start video generation
        // and wait for navigation to the video post URL
        const hasI2VBtn = await page.evaluate(`
            !!Array.from(document.querySelectorAll('button')).find(b =>
                (b.getAttribute('aria-label') || '').includes('製作影片') ||
                (b.textContent || '').includes('製作影片')
            )
        `);
        if (hasI2VBtn) {
            await page.evaluate(`
                Array.from(document.querySelectorAll('button')).find(b =>
                    (b.getAttribute('aria-label') || '').includes('製作影片') ||
                    (b.textContent || '').includes('製作影片')
                ).click()
            `);
            // Wait for navigation to the new video post URL
            let navigated = false;
            const currentUrl = await page.evaluate(`window.location.href`);
            const videoWaitStart = Date.now();
            while (Date.now() - videoWaitStart < 30000) {
                await page.wait(2);
                const newHref = await page.evaluate(`window.location.href`);
                if (newHref !== currentUrl && newHref.includes('/imagine/post/')) {
                    navigated = true;
                    break;
                }
            }
            if (navigated) {
                // Re-install blob interceptor on new video post page
                await page.evaluate(BLOB_INTERCEPTOR);
            }
        }

        // Wait for download button to appear (indicates video is ready)
        let dlClicked = false;
        while (Date.now() - startTime < timeoutMs) {
            await page.wait(5);
            dlClicked = await page.evaluate(`
                (function() {
                    const dlBtn = Array.from(document.querySelectorAll('button')).find(b =>
                        (b.getAttribute('aria-label') || '').includes('下載')
                    );
                    if (!dlBtn || dlBtn.disabled) return false;
                    dlBtn.click();
                    return true;
                })()
            `);
            if (dlClicked) break;
        }

        if (!dlClicked) return [{ file: '[TIMEOUT] download button not found after ' + Math.round((Date.now() - startTime) / 1000) + 's' }];

        mkdirSync(outputDir, { recursive: true });
        const timestamp = Date.now();
        const safePrompt = prompt.replace(/[^a-zA-Z0-9]/g, '_').slice(0, 30);
        const filename = `grok_video_${safePrompt}_${timestamp}.mp4`;
        const filePath = join(outputDir, filename);

        // Strategy 1: find <a href="...mp4"> download anchor — Node.js direct download (CDN-safe)
        await page.wait(2);
        const anchorUrl = await page.evaluate(`
            (function() {
                const anchors = Array.from(document.querySelectorAll('a[href]'));
                for (const a of anchors) {
                    const href = a.href || '';
                    if (href.includes('.mp4') || (a.download && href.startsWith('http'))) return href;
                }
                return null;
            })()
        `);
        if (anchorUrl) {
            try {
                await downloadUrl(anchorUrl, filePath);
                return [{ file: filePath }];
            } catch(e) { /* fall through */ }
        }

        // Strategy 2: fetch video src from <video> element via browser fetch
        let b64Data = null;
        const videoUrl = await page.evaluate(`
            (function() {
                const videos = Array.from(document.querySelectorAll('video'));
                for (const v of videos) {
                    const src = v.src || v.currentSrc || v.getAttribute('src') || '';
                    if (src.startsWith('http') || src.startsWith('blob:')) return src;
                }
                return null;
            })()
        `);

        if (videoUrl && videoUrl.startsWith('http')) {
            // Try Node.js download first (avoids CORS issues)
            try {
                await downloadUrl(videoUrl, filePath);
                if (require('fs').existsSync(filePath) && require('fs').statSync(filePath).size > 1000) {
                    return [{ file: filePath }];
                }
            } catch(e) { /* fall through to browser fetch */ }

            // Try browser fetch with session cookies (for authenticated URLs like assets.grok.com)
            try {
                b64Data = await page.evaluate(async (url) => {
                    try {
                        const resp = await fetch(url, { credentials: 'include' });
                        if (!resp.ok) return null;
                        const buf = await resp.arrayBuffer();
                        const bytes = new Uint8Array(buf);
                        let binary = '';
                        for (let i = 0; i < bytes.byteLength; i += 1024) {
                            const chunk = bytes.subarray(i, i + 1024);
                            binary += String.fromCharCode.apply(null, chunk);
                        }
                        return 'data:video/mp4;base64,' + btoa(binary);
                    } catch(e) { return null; }
                }, videoUrl);
                if (b64Data) return [{ file: '[INFO] downloaded via browser fetch' }];
            } catch(e) { /* fall through */ }
        }

        if (videoUrl) {
            b64Data = await page.evaluate(`
                (async function() {
                    try {
                        const resp = await fetch(${JSON.stringify(videoUrl)}, { credentials: 'include' });
                        if (!resp.ok) return null;
                        const buf = await resp.arrayBuffer();
                        const bytes = new Uint8Array(buf);
                        let binary = '';
                        for (let i = 0; i < bytes.byteLength; i += 1024) {
                            const chunk = bytes.subarray(i, i + 1024);
                            binary += String.fromCharCode.apply(null, chunk);
                        }
                        return 'data:video/mp4;base64,' + btoa(binary);
                    } catch(e) { return null; }
                })()
            `);
        }

        // Strategy 3: blob interceptor fallback
        if (!b64Data) {
            const dlStart = Date.now();
            while (Date.now() - dlStart < 60000) {
                await page.wait(3);
                const result = await page.evaluate(`
                    (function() {
                        const d = (window.__downloadData || []).find(x => x.type && (x.type.includes('mp4') || x.type.includes('video')));
                        return d ? { b64: d.b64 } : null;
                    })()
                `);
                if (result && result.b64) { b64Data = result.b64; break; }
            }
        }

        if (!b64Data) return [{ file: '[ERROR] blob data not captured' }];

        const base64 = b64Data.replace(/^data:[^;]+;base64,/, '');
        writeFileSync(filePath, Buffer.from(base64, 'base64'));
        return [{ file: filePath }];
    },
});
