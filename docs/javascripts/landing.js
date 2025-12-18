// Homepage animations - Typewriter and Canvas background
console.log('landing.js loaded');

let typewriterInstance = null;
let canvasAnimationId = null;
let canvasCtx = null;
let canvasParticles = [];
let canvasWidth = 0;
let canvasHeight = 0;
let initialized = false;
let mouseX = undefined;
let mouseY = undefined;
let lastFrameTime = performance.now(); // Use performance.now() for better precision
let watchdogInterval = null;
let eventListenersAttached = false;

// Wait for element to exist
function waitForElement(id, callback, maxAttempts = 50) {
    const element = document.getElementById(id);
    if (element) {
        callback(element);
    } else if (maxAttempts > 0) {
        setTimeout(() => waitForElement(id, callback, maxAttempts - 1), 100);
    }
}

// Wait for Typewriter library to load
function waitForTypewriter(callback, maxAttempts = 50) {
    if (typeof Typewriter !== 'undefined') {
        callback();
    } else if (maxAttempts > 0) {
        setTimeout(() => waitForTypewriter(callback, maxAttempts - 1), 100);
    }
}

// Initialize Typewriter effect
function initTypewriter() {
    waitForElement('typewriter-target', (target) => {
        console.log('Typewriter target found');
        // Stop existing instance
        if (typewriterInstance) {
            try {
                typewriterInstance.stop();
            } catch (e) { }
            typewriterInstance = null;
        }

        waitForTypewriter(() => {
            console.log('Typewriter library loaded, initializing...');
            target.innerHTML = '';
            try {
                typewriterInstance = new Typewriter(target, {
                    loop: true,
                    delay: 50,
                    cursorClassName: 'cursor'
                });

                typewriterInstance
                    .typeString('<span class="prompt">➜</span> <span class="command">hector serve --config agents.yaml</span><br>')
                    .pauseFor(500)
                    .typeString('<span class="output">INFO  [10:23:01] Loading configuration from agents.yaml</span><br>')
                    .typeString('<span class="output">INFO  [10:23:01] Initializing Agent Mesh...</span><br>')
                    .pauseFor(300)
                    .typeString('<span class="output">INFO  [10:23:02] Agent "Research" connected (Model: gpt-4o)</span><br>')
                    .typeString('<span class="output">INFO  [10:23:02] Agent "Writer" connected (Model: claude-3-opus)</span><br>')
                    .typeString('<span class="output">INFO  [10:23:02] <strong>Server ready on http://localhost:8080</strong> 🚀</span><br>')
                    .pauseFor(2000)
                    .deleteAll(10)
                    .start();
                console.log('Typewriter started successfully');
            } catch (e) {
                console.error('Typewriter error:', e);
            }
        });
    });
}

// Canvas particle class
class Particle {
    constructor() {
        this.x = Math.random() * canvasWidth;
        this.y = Math.random() * canvasHeight;
        this.vx = (Math.random() - 0.5) * 0.6;
        this.vy = (Math.random() - 0.5) * 0.6;
        this.size = Math.random() * 1.5 + 1.5; // Slightly larger for visibility
        this.baseSize = this.size;
        // Brighter, more glowy colors
        this.color = Math.random() > 0.5 ? 'rgba(16, 185, 129, 0.9)' : 'rgba(59, 130, 246, 0.9)';
        this.pulse = Math.random() * Math.PI * 2;
    }

    update(mouseX, mouseY, deltaTime = 1) {
        // Normalize deltaTime to 60fps baseline (16.67ms per frame)
        const dt = Math.min(deltaTime / 16.67, 3); // Cap at 3x to prevent huge jumps

        // Always update pulse first for breathing effect
        this.pulse += 0.02 * dt;

        // CRITICAL: Update position with velocity - particles MUST move
        this.x += this.vx * dt;
        this.y += this.vy * dt;

        // Enhanced mouse interaction - particles move away from mouse with stronger force
        if (mouseX !== undefined && mouseY !== undefined) {
            const dx = this.x - mouseX;
            const dy = this.y - mouseY;
            const dist = Math.sqrt(dx * dx + dy * dy);
            const maxDist = 200; // Larger interaction radius

            if (dist < maxDist && dist > 0) {
                const force = Math.pow((maxDist - dist) / maxDist, 2) * 0.3; // Stronger, smoother force
                this.vx += (dx / dist) * force * dt;
                this.vy += (dy / dist) * force * dt;

                // Also increase size when near mouse
                this.size = this.baseSize + Math.sin(this.pulse) * 0.5 + (maxDist - dist) / maxDist * 1.5;
            } else {
                this.size = this.baseSize + Math.sin(this.pulse) * 0.5;
            }
        } else {
            // Normal pulse effect
            this.size = this.baseSize + Math.sin(this.pulse) * 0.5;
        }

        // Apply damping BEFORE clamping to prevent velocity accumulation
        this.vx *= 0.998;
        this.vy *= 0.998;

        // Cap maximum velocity to prevent speed-up over time
        const maxVelocity = 2.0;
        const speed = Math.sqrt(this.vx * this.vx + this.vy * this.vy);
        if (speed > maxVelocity) {
            this.vx = (this.vx / speed) * maxVelocity;
            this.vy = (this.vy / speed) * maxVelocity;
        }

        // Ensure minimum velocity to keep particles moving
        const minVelocity = 0.05;
        if (speed < minVelocity && speed > 0) {
            this.vx = (this.vx / speed) * minVelocity;
            this.vy = (this.vy / speed) * minVelocity;
        } else if (speed === 0) {
            // Restart stopped particles with random velocity
            this.vx = (Math.random() - 0.5) * 0.3;
            this.vy = (Math.random() - 0.5) * 0.3;
        }

        // Boundary wrapping
        if (this.x < 0) this.x = canvasWidth;
        if (this.x > canvasWidth) this.x = 0;
        if (this.y < 0) this.y = canvasHeight;
        if (this.y > canvasHeight) this.y = 0;
    }

    draw(ctx) {
        // Sharp, bright star-like particle - optimized to prevent memory leaks
        ctx.save();

        // Small outer glow for visibility (minimal) - use solid colors instead of gradients
        const glowSize = this.size * 1.5;

        // Outer glow layer (most transparent)
        ctx.globalAlpha = 0.1;
        ctx.fillStyle = this.color;
        ctx.beginPath();
        ctx.arc(this.x, this.y, glowSize * 1.2, 0, Math.PI * 2);
        ctx.fill();

        // Middle glow layer
        ctx.globalAlpha = 0.2;
        ctx.beginPath();
        ctx.arc(this.x, this.y, glowSize, 0, Math.PI * 2);
        ctx.fill();

        // Sharp, bright core - star-like
        ctx.globalAlpha = 1.0;
        ctx.beginPath();
        ctx.arc(this.x, this.y, this.size, 0, Math.PI * 2);
        ctx.fillStyle = this.color;
        ctx.fill();

        // Bright white center for star effect
        ctx.beginPath();
        ctx.arc(this.x, this.y, this.size * 0.4, 0, Math.PI * 2);
        ctx.fillStyle = '#ffffff';
        ctx.fill();

        ctx.restore();
    }
}

// Initialize canvas particles
function initCanvas() {
    waitForElement('ambient-canvas', (canvas) => {
        console.log('Canvas found');
        // Stop existing animation
        if (canvasAnimationId) {
            cancelAnimationFrame(canvasAnimationId);
            canvasAnimationId = null;
        }

        canvasCtx = canvas.getContext('2d');

        function resize() {
            canvasWidth = canvas.width = window.innerWidth;
            canvasHeight = canvas.height = window.innerHeight;
            // Ensure canvas has valid dimensions
            if (canvasWidth === 0 || canvasHeight === 0) {
                canvasWidth = canvas.width = window.innerWidth || 1920;
                canvasHeight = canvas.height = window.innerHeight || 1080;
            }
        }

        function initParticles() {
            canvasParticles = [];
            // Less dense particles for brittleness
            const count = Math.floor((canvasWidth * canvasHeight) / 12000);
            // Fewer particles for brittleness
            const minParticles = 50;
            const particleCount = Math.max(count, minParticles);
            console.log('Creating', particleCount, 'particles');
            for (let i = 0; i < particleCount; i++) {
                canvasParticles.push(new Particle());
            }
        }

        // Track last frame time for delta time calculation
        let lastAnimFrameTime = performance.now();
        let watchdogInterval = null; // Declare watchdogInterval here

        function animate(timestamp) {
            // Always request next frame FIRST to ensure loop never stops
            canvasAnimationId = requestAnimationFrame(animate);

            // Use requestAnimationFrame timestamp for precise delta time
            if (!timestamp) timestamp = performance.now();
            const deltaTime = timestamp - lastAnimFrameTime;
            lastAnimFrameTime = timestamp;

            // Skip frames with huge delta (tab was hidden) to prevent jumps
            if (deltaTime > 200) return;

            try {
                if (!canvasCtx || canvasWidth === 0 || canvasHeight === 0) {
                    // Re-initialize if canvas is invalid
                    resize();
                    if (canvasWidth === 0 || canvasHeight === 0) {
                        return;
                    }
                }

                canvasCtx.clearRect(0, 0, canvasWidth, canvasHeight);

                // Update and draw particles with delta time
                for (let i = 0; i < canvasParticles.length; i++) {
                    const p = canvasParticles[i];
                    p.update(mouseX, mouseY, deltaTime);
                    p.draw(canvasCtx);

                    // Draw connections - more visible and dense
                    for (let j = i + 1; j < canvasParticles.length; j++) {
                        const p2 = canvasParticles[j];
                        const dx = p.x - p2.x;
                        const dy = p.y - p2.y;
                        const dist = Math.sqrt(dx * dx + dy * dy);

                        // Increased connection distance for denser network
                        if (dist < 180) {
                            canvasCtx.beginPath();
                            const opacity = Math.max(0.08, 0.2 - dist / 1200);
                            canvasCtx.strokeStyle = `rgba(255, 255, 255, ${opacity})`;
                            canvasCtx.lineWidth = 1;
                            canvasCtx.moveTo(p.x, p.y);
                            canvasCtx.lineTo(p2.x, p2.y);
                            canvasCtx.stroke();
                        }
                    }
                }
            } catch (e) {
                console.error('Animation error:', e);
                // Continue animation even on error
            }
        }

        // Clear existing watchdog before creating new one
        if (watchdogInterval) {
            clearInterval(watchdogInterval);
            watchdogInterval = null;
        }

        // Watchdog to ensure animation never stops
        watchdogInterval = setInterval(() => {
            const now = performance.now();
            // Check if animation stopped (no frame updates in 2 seconds)
            if (now - lastAnimFrameTime > 2000) {
                if (canvasAnimationId === null) {
                    console.log('Animation stopped, restarting...');
                    start();
                } else {
                    // Animation ID exists but no frames - restart
                    console.log('Animation stalled, restarting...');
                    canvasAnimationId = null;
                    start();
                }
            }
        }, 1000);

        function start() {
            // Always restart animation, don't check if already running
            if (canvasAnimationId) {
                cancelAnimationFrame(canvasAnimationId);
                canvasAnimationId = null;
            }
            console.log('Starting canvas animation');
            resize();
            console.log('Canvas size:', canvasWidth, 'x', canvasHeight);
            initParticles();
            console.log('Particles initialized:', canvasParticles.length);
            // Ensure animation starts
            if (!canvasAnimationId) {
                animate();
            }
        }

        function stop() {
            if (canvasAnimationId) {
                cancelAnimationFrame(canvasAnimationId);
                canvasAnimationId = null;
            }
        }

        // Only attach event listeners once to prevent memory leaks
        if (!eventListenersAttached) {
            eventListenersAttached = true;

            // Enhanced mouse tracking for interactivity
            const handleMouseMove = (e) => {
                mouseX = e.clientX;
                mouseY = e.clientY;
            };

            const handleMouseLeave = () => {
                mouseX = undefined;
                mouseY = undefined;
            };

            const handleTouchMove = (e) => {
                if (e.touches.length > 0) {
                    mouseX = e.touches[0].clientX;
                    mouseY = e.touches[0].clientY;
                }
            };

            const handleTouchEnd = () => {
                mouseX = undefined;
                mouseY = undefined;
            };

            canvas.addEventListener('mousemove', handleMouseMove);
            canvas.addEventListener('mouseleave', handleMouseLeave);
            canvas.addEventListener('touchmove', handleTouchMove, { passive: true });
            canvas.addEventListener('touchend', handleTouchEnd);
            canvas.addEventListener('touchcancel', handleTouchEnd);

            // Handle resize
            window.addEventListener('resize', () => {
                resize();
                initParticles();
            });

            // Handle visibility - restart animation when visible
            document.addEventListener('visibilitychange', () => {
                if (document.hidden) {
                    stop();
                } else {
                    // Restart when visible again
                    setTimeout(() => {
                        if (!canvasAnimationId) {
                            start();
                        }
                    }, 100);
                }
            });

            // Also watch for focus/blur to ensure animation continues
            window.addEventListener('focus', () => {
                if (!canvasAnimationId) {
                    start();
                }
            });
        }

        start();
    });
}

// Main initialization function
function initHomepageAnimations() {
    if (initialized) {
        console.log('Already initialized');
        return;
    }

    const hasCanvas = document.getElementById('ambient-canvas');
    const hasTypewriter = document.getElementById('typewriter-target');

    console.log('Checking elements:', { hasCanvas: !!hasCanvas, hasTypewriter: !!hasTypewriter });

    if (!hasCanvas && !hasTypewriter) {
        console.log('Not on homepage, skipping');
        return; // Not on homepage
    }

    initialized = true;
    console.log('Initializing homepage animations...');
    initTypewriter();
    initCanvas();
}

// Initialize when DOM is ready
function ready(fn) {
    if (document.readyState !== 'loading') {
        fn();
    } else {
        document.addEventListener('DOMContentLoaded', fn);
    }
}

// Initial attempts
ready(() => {
    console.log('DOM ready, attempting initialization...');
    setTimeout(initHomepageAnimations, 100);
    setTimeout(initHomepageAnimations, 500);
    setTimeout(initHomepageAnimations, 1000);
});

// Watch for navigation (SPA)
let lastUrl = location.href;
const checkNavigation = () => {
    if (location.href !== lastUrl) {
        lastUrl = location.href;
        initialized = false;
        setTimeout(initHomepageAnimations, 200);
    }
};

window.addEventListener('hashchange', checkNavigation);
window.addEventListener('popstate', checkNavigation);

// Watch for DOM changes
if (document.body) {
    const observer = new MutationObserver(() => {
        checkNavigation();
        if (document.getElementById('ambient-canvas') || document.getElementById('typewriter-target')) {
            if (!initialized) {
                setTimeout(initHomepageAnimations, 100);
            }
        }
    });
    observer.observe(document.body, { childList: true, subtree: true });
}

// Also try after a delay as fallback
setTimeout(() => {
    console.log('Fallback initialization attempt');
    initHomepageAnimations();
}, 2000);
