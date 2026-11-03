# Domain Companion: Frontend & Client Applications

Apply this companion when auditing browser client apps, SPAs (React, Vue, Svelte, Angular), mobile apps (React Native, Flutter, Swift, Kotlin), or desktop apps (Electron, Tauri).

## Core Disciplines

1. **Rendering Performance & Cascade Hazards**:
   - Accidental component re-render loops caused by unstable object references or functions in props/context.
   - Heavy synchronous computations (parsing large JSON, crypto, layout calculations) executed on the main UI thread rather than via Web Workers.
2. **Memory Leaks in Subscriptions & Listeners**:
   - Event listeners (`window.addEventListener`), WebSocket subscriptions, intervals (`setInterval`), and RxJS observables not unsubscribed upon component unmount.
3. **State Synchronization & Hydration**:
   - Hydration mismatch hazards in SSR frameworks (Next.js, Nuxt, SvelteKit) accessing `window` or non-deterministic state during server render.
   - Split-brain UI state when duplicating server responses across multiple local store slices without a single source of truth.
4. **Network & Request Lifecycle**:
   - Missing request cancellation (`AbortController`) when navigating between screens, causing outdated API responses to overwrite newer state.
