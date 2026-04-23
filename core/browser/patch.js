// Stealth patches injected via EvalOnNewDocument.
// Arguments are injected as a leading const block by the Go caller:
//   const __langs = [...];   // navigator_langs from profile
//   const __w = 1920;        // viewport width
//   const __h = 1080;        // viewport height
(() => {
  'use strict';

  // --- navigator.language / navigator.languages ---
  //
  // CDP Network.setUserAgentOverride(acceptLanguage) sets the HTTP header but
  // does NOT update navigator.language / navigator.languages in JS. Those are
  // read from the browser profile at context creation. On Linux headless they
  // reflect the system ICU locale (usually "en-US" regardless of profile).
  //
  // We patch them here. The key to being undetectable: define with a getter
  // first (requires configurable:true), then immediately lock the descriptor
  // back to configurable:false so it looks exactly like a native property.
  const primary = __langs.length ? __langs[0] : 'en-US';

  const sealGetter = (target, prop, fn) => {
    try {
      Object.defineProperty(target, prop, {
        get: fn,
        set: undefined,
        enumerable: true,
        configurable: true,   // must be true to set a getter
      });
      Object.defineProperty(target, prop, {
        configurable: false,  // seal: now indistinguishable from native
      });
    } catch (_) {}
  };

  const patchLangs = (target) => {
    if (!target) return;
    sealGetter(target, 'language', () => primary);
    sealGetter(target, 'languages', () => Object.freeze(__langs.slice()));
  };

  // Patch the navigator instance (Linux headless stores own-props here)
  // and Navigator.prototype (other platforms / future Chrome versions).
  patchLangs(navigator);
  patchLangs(Object.getPrototypeOf(navigator));
  if (typeof WorkerNavigator !== 'undefined') {
    patchLangs(WorkerNavigator.prototype);
  }

  // --- screen dimensions ---
  //
  // EmulationSetDeviceMetricsOverride sets the CSS viewport but leaves
  // window.screen.* at headless defaults. Checkers compare screen size
  // against viewport and flag the mismatch as automation.
  //
  // Screen properties live on Screen.prototype as non-configurable getters.
  // Patching the prototype makes them look native.
  const patchScreen = () => {
    const proto = typeof Screen !== 'undefined' ? Screen.prototype : null;
    if (!proto) return;
    sealGetter(proto, 'width',       () => __w);
    sealGetter(proto, 'height',      () => __h);
    sealGetter(proto, 'availWidth',  () => __w);
    sealGetter(proto, 'availHeight', () => __h);
    sealGetter(proto, 'availLeft',   () => 0);
    sealGetter(proto, 'availTop',    () => 0);
  };
  patchScreen();

  // window.outerWidth/Height are own configurable properties; Chrome normally
  // sets them to match the OS window size. In headless they are 0.
  sealGetter(window, 'outerWidth',  () => __w);
  sealGetter(window, 'outerHeight', () => __h);
})();
