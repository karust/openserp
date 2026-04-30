// Stealth patches injected via EvalOnNewDocument.
// Arguments are injected as a leading const block by the Go caller:
//   const __langs = [...];          // navigator_langs from profile
//   const __w = 1920;               // viewport/content width
//   const __h = 955;                // viewport/content height
//   const __screenW = 1920;         // screen.width
//   const __screenH = 1080;         // screen.height
//   const __availW = 1920;          // screen.availWidth
//   const __availH = 1040;          // screen.availHeight
//   const __availTop = 0;           // screen.availTop
//   const __outerW = 1920;          // window.outerWidth
//   const __outerH = 1040;          // window.outerHeight
//   const __webglVendor = "...";    // UNMASKED_VENDOR_WEBGL spoof
//   const __webglRenderer = "...";  // UNMASKED_RENDERER_WEBGL spoof
//
// Scope: only patches that fix detectors with high ROI and low introspection
// surface. Notably absent: Function.prototype.toString proxy, custom Worker
// constructor, mass plugin/mimeType arrays, iframe contentWindow patching.
// Those triggered server-side detection on Google in commit 612e0dc.
(() => {
  'use strict';

  const sealGetter = (target, prop, fn) => {
    try {
      Object.defineProperty(target, prop, {
        get: fn,
        set: undefined,
        enumerable: true,
        configurable: true,
      });
      Object.defineProperty(target, prop, { configurable: false });
    } catch (_) {}
  };

  // --- navigator.webdriver ---
  // headless Chrome sets this to true. Delete it so getter returns undefined.
  // (We rely on `disable-blink-features=AutomationControlled` already turning
  // this off via the launcher; this is belt-and-braces in case it leaks.)
  try {
    if (typeof navigator.webdriver !== 'undefined') {
      delete Object.getPrototypeOf(navigator).webdriver;
    }
  } catch (_) {}

  // --- navigator.language / navigator.languages ---
  // CDP setUserAgentOverride sets the HTTP header but not these JS props.
  const primary = __langs.length ? __langs[0] : 'en-US';
  const patchLangs = (target) => {
    if (!target) return;
    sealGetter(target, 'language', () => primary);
    sealGetter(target, 'languages', () => Object.freeze(__langs.slice()));
  };
  patchLangs(Object.getPrototypeOf(navigator));
  if (typeof WorkerNavigator !== 'undefined') {
    patchLangs(WorkerNavigator.prototype);
  }

  // --- screen dimensions ---
  // EmulationSetDeviceMetricsOverride sets the CSS viewport but leaves
  // window.screen.* at headless defaults.
  const screenProto = typeof Screen !== 'undefined' ? Screen.prototype : null;
  if (screenProto) {
    sealGetter(screenProto, 'width', () => __screenW);
    sealGetter(screenProto, 'height', () => __screenH);
    sealGetter(screenProto, 'availWidth', () => __availW);
    sealGetter(screenProto, 'availHeight', () => __availH);
    sealGetter(screenProto, 'availLeft', () => 0);
    sealGetter(screenProto, 'availTop', () => __availTop);
    // Headless reports colorDepth/pixelDepth=24 already in most builds, but
    // some checks see 0 in WSL/Docker. Lock to 24 which matches real Chrome.
    sealGetter(screenProto, 'colorDepth', () => 24);
    sealGetter(screenProto, 'pixelDepth', () => 24);
  }
  if (typeof window !== 'undefined') {
    sealGetter(window, 'outerWidth', () => __outerW);
    sealGetter(window, 'outerHeight', () => __outerH);
  }

  // --- WebGL vendor/renderer spoof ---
  // Patches getParameter(37445=UNMASKED_VENDOR_WEBGL, 37446=UNMASKED_RENDERER_WEBGL)
  // on WebGLRenderingContext.prototype and WebGL2RenderingContext.prototype.
  // Only delegates to the original for all other params, so behavioral
  // signals (extension list, real pixel rendering) still pass through.
  const patchWebGLProto = (proto) => {
    if (!proto) return;
    const original = proto.getParameter;
    if (typeof original !== 'function') return;
    const replacement = function getParameter(parameter) {
      if (parameter === 37445) return __webglVendor;
      if (parameter === 37446) return __webglRenderer;
      return original.apply(this, arguments);
    };
    try {
      Object.defineProperty(proto, 'getParameter', {
        value: replacement,
        writable: true,
        enumerable: false,
        configurable: true,
      });
    } catch (_) {}
  };
  if (typeof WebGLRenderingContext !== 'undefined') {
    patchWebGLProto(WebGLRenderingContext.prototype);
  }
  if (typeof WebGL2RenderingContext !== 'undefined') {
    patchWebGLProto(WebGL2RenderingContext.prototype);
  }

  // --- navigator.plugins / mimeTypes ---
  // Match real Chrome 136 exactly: 5 plugins (all "internal-pdf-viewer"), each
  // exposing application/pdf + text/pdf. navigator.mimeTypes dedupes to 2.
  // Each MimeType.enabledPlugin must back-reference the FIRST plugin that owns
  // that type (Chrome's invariant). Plugin order is fixed.
  try {
    const PluginArrayProto = typeof PluginArray !== 'undefined' ? PluginArray.prototype : null;
    const PluginProto = typeof Plugin !== 'undefined' ? Plugin.prototype : null;
    const MimeTypeArrayProto = typeof MimeTypeArray !== 'undefined' ? MimeTypeArray.prototype : null;
    const MimeTypeProto = typeof MimeType !== 'undefined' ? MimeType.prototype : null;

    if (PluginArrayProto && PluginProto && MimeTypeArrayProto && MimeTypeProto) {
      const pluginNames = [
        'PDF Viewer',
        'Chrome PDF Viewer',
        'Chromium PDF Viewer',
        'Microsoft Edge PDF Viewer',
        'WebKit built-in PDF',
      ];
      const mimeSpecs = [
        { type: 'application/pdf', suffixes: 'pdf', description: 'Portable Document Format' },
        { type: 'text/pdf', suffixes: 'pdf', description: 'Portable Document Format' },
      ];

      // Two MimeType instances, each enabledPlugin points to the first plugin
      // (PDF Viewer) per Chrome's invariant: navigator.mimeTypes[i].enabledPlugin
      // === navigator.plugins[0] for every PDF mime.
      const sharedMimes = mimeSpecs.map((spec) => {
        const m = Object.create(MimeTypeProto);
        Object.defineProperty(m, 'type', { value: spec.type, enumerable: true });
        Object.defineProperty(m, 'suffixes', { value: spec.suffixes, enumerable: true });
        Object.defineProperty(m, 'description', { value: spec.description, enumerable: true });
        return m;
      });

      const plugins = pluginNames.map((name) => {
        const p = Object.create(PluginProto);
        Object.defineProperty(p, 'name', { value: name, enumerable: true });
        Object.defineProperty(p, 'filename', { value: 'internal-pdf-viewer', enumerable: true });
        Object.defineProperty(p, 'description', { value: 'Portable Document Format', enumerable: true });
        Object.defineProperty(p, 'length', { value: sharedMimes.length, enumerable: true });
        sharedMimes.forEach((m, i) => {
          Object.defineProperty(p, String(i), { value: m, enumerable: true });
          Object.defineProperty(p, m.type, { value: m });
        });
        return p;
      });

      // Set enabledPlugin AFTER plugins are constructed, pointing to plugins[0].
      sharedMimes.forEach((m) => {
        Object.defineProperty(m, 'enabledPlugin', { value: plugins[0], enumerable: true });
      });

      const pluginArr = Object.create(PluginArrayProto);
      Object.defineProperty(pluginArr, 'length', { value: plugins.length, enumerable: true });
      plugins.forEach((p, i) => {
        Object.defineProperty(pluginArr, String(i), { value: p, enumerable: true });
        Object.defineProperty(pluginArr, p.name, { value: p });
      });

      const mimeArr = Object.create(MimeTypeArrayProto);
      Object.defineProperty(mimeArr, 'length', { value: sharedMimes.length, enumerable: true });
      sharedMimes.forEach((m, i) => {
        Object.defineProperty(mimeArr, String(i), { value: m, enumerable: true });
        Object.defineProperty(mimeArr, m.type, { value: m });
      });

      sealGetter(Object.getPrototypeOf(navigator), 'plugins', () => pluginArr);
      sealGetter(Object.getPrototypeOf(navigator), 'mimeTypes', () => mimeArr);
    }
  } catch (_) {}

  // --- navigator.permissions.query notifications fix ---
  // headless returns 'denied' for notifications when Notification.permission is
  // 'default'. Real Chrome returns 'prompt' in that case. Sannysoft checks this
  // mismatch (permissions_new / headchr_permissions).
  try {
    if (navigator.permissions && typeof navigator.permissions.query === 'function') {
      const proto = Object.getPrototypeOf(navigator.permissions);
      const desc = Object.getOwnPropertyDescriptor(proto, 'query');
      if (desc && typeof desc.value === 'function') {
        const original = desc.value;
        const replacement = function query(parameters) {
          if (parameters && parameters.name === 'notifications' &&
              typeof Notification !== 'undefined' && Notification.permission === 'default') {
            return Promise.resolve({ state: 'prompt', onchange: null });
          }
          return original.apply(this, arguments);
        };
        Object.defineProperty(proto, 'query', {
          value: replacement,
          writable: desc.writable,
          enumerable: desc.enumerable,
          configurable: desc.configurable,
        });
      }
    }
  } catch (_) {}

  // --- getBoundingClientRect / getClientRects subpixel jitter ---
  // Headless Chrome returns integer-valued rects; real Chrome returns subpixel
  // floats due to CSS layout fractions. Fingerprinters hash rect tuples; even
  // a sub-pixel offset breaks the canonical "headless rect" hash.
  // Jitter is deterministic per-element (based on element identity) so the
  // same element returns the same value across calls within the page lifetime.
  try {
    const rectProto = typeof DOMRect !== 'undefined' ? DOMRect.prototype : null;
    const elProto = typeof Element !== 'undefined' ? Element.prototype : null;
    if (rectProto && elProto) {
      const wmJitter = new WeakMap();
      const jitterFor = (el) => {
        let j = wmJitter.get(el);
        if (!j) {
          // Tiny noise in [-0.05, +0.05) — well below visual threshold but
          // changes hash output. Generated once per element.
          j = {
            x: (Math.random() - 0.5) * 0.1,
            y: (Math.random() - 0.5) * 0.1,
          };
          wmJitter.set(el, j);
        }
        return j;
      };
      const origGBCR = elProto.getBoundingClientRect;
      Object.defineProperty(elProto, 'getBoundingClientRect', {
        value: function getBoundingClientRect() {
          const r = origGBCR.apply(this, arguments);
          const j = jitterFor(this);
          // DOMRect is mutable; nudge x/y. width/height left intact so layout
          // calculations don't drift.
          try { r.x = r.x + j.x; r.y = r.y + j.y; } catch (_) {}
          return r;
        },
        writable: true,
        enumerable: false,
        configurable: true,
      });
    }
  } catch (_) {}

  // --- window.chrome.runtime stub ---
  // Real Chrome exposes window.chrome with a .runtime sub-object.
  // headless leaves window.chrome empty, which sannysoft (chrome_new,
  // headchr_chrome_obj) flags. A minimal runtime stub satisfies the check
  // without touching method behavior.
  try {
    if (typeof window !== 'undefined') {
      if (!window.chrome) {
        Object.defineProperty(window, 'chrome', { value: {}, writable: true, configurable: true });
      }
      if (window.chrome && !window.chrome.runtime) {
        Object.defineProperty(window.chrome, 'runtime', {
          value: {
            OnInstalledReason: { CHROME_UPDATE: 'chrome_update', INSTALL: 'install', UPDATE: 'update' },
            OnRestartRequiredReason: { APP_UPDATE: 'app_update', OS_UPDATE: 'os_update', PERIODIC: 'periodic' },
            PlatformOs: { ANDROID: 'android', CROS: 'cros', LINUX: 'linux', MAC: 'mac', WIN: 'win' },
          },
          writable: true,
          enumerable: true,
          configurable: true,
        });
      }
    }
  } catch (_) {}
})();
