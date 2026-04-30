async () => {
  const workerData = await new Promise((resolve) => {
    try {
      const source = [
        "self.onmessage = () => {",
        "let webGLVendor = '';",
        "let webGLRenderer = '';",
        "try {",
        "const canvas = typeof OffscreenCanvas !== 'undefined' ? new OffscreenCanvas(1, 1) : null;",
        "const gl = canvas ? (canvas.getContext('webgl') || canvas.getContext('experimental-webgl') || canvas.getContext('webgl2')) : null;",
        "const debugInfo = gl && gl.getExtension('WEBGL_debug_renderer_info');",
        "if (gl && debugInfo) {",
        "webGLVendor = gl.getParameter(debugInfo.UNMASKED_VENDOR_WEBGL) || '';",
        "webGLRenderer = gl.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL) || '';",
        "}",
        "} catch (_) {}",
        "self.postMessage({",
        "userAgent: self.navigator.userAgent || '',",
        "platform: self.navigator.platform || '',",
        "navigatorLanguages: Array.from(self.navigator.languages || []),",
        "timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || '',",
        "webGLVendor,",
        "webGLRenderer,",
        "});",
        "};",
      ].join("\n");
      const blob = new Blob([source], { type: "application/javascript" });
      const url = URL.createObjectURL(blob);
      const worker = new Worker(url);
      worker.onmessage = (event) => {
        resolve(event.data || {});
        worker.terminate();
        URL.revokeObjectURL(url);
      };
      worker.onerror = () => {
        resolve({});
        worker.terminate();
        URL.revokeObjectURL(url);
      };
      worker.postMessage("run");
    } catch (_) {
      resolve({});
    }
  });

  return {
    userAgent: navigator.userAgent || "",
    platform: navigator.userAgentData ? (navigator.userAgentData.platform || "") : "",
    navigatorPlatform: navigator.platform || "",
    navigatorLanguages: Array.from(navigator.languages || []),
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "",
    locale: Intl.DateTimeFormat().resolvedOptions().locale || "",
    webdriverType: typeof navigator.webdriver,
    webdriverOwnPropPresent: Object.getOwnPropertyNames(navigator).includes("webdriver"),
    workerUserAgent: workerData.userAgent || "",
    workerPlatform: workerData.platform || "",
    workerNavigatorLangs: Array.from(workerData.navigatorLanguages || []),
    workerTimezone: workerData.timezone || "",
    workerWebGLVendor: workerData.webGLVendor || "",
    workerWebGLRenderer: workerData.webGLRenderer || "",
    innerHeight: window.innerHeight || 0,
    outerHeight: window.outerHeight || 0,
    screenHeight: window.screen ? (window.screen.height || 0) : 0,
    screenAvailHeight: window.screen ? (window.screen.availHeight || 0) : 0,
  };
}
