function initApp() {
  // CSRF Protection Handler: Attach CSRF token to same-origin non-GET fetch requests and forms
  const csrfMeta = document.querySelector('meta[name="csrf-token"]');
  const csrfToken = csrfMeta ? csrfMeta.getAttribute('content') : '';
  if (csrfToken) {
    const originalFetch = window.fetch;
    window.fetch = function (url, options = {}) {
      const method = (options.method || 'GET').toUpperCase();
      if (method !== 'GET' && method !== 'HEAD') {
        const isSameOrigin = typeof url === 'string' && (url.startsWith('/') || url.startsWith(window.location.origin));
        if (isSameOrigin) {
          if (!options.headers) {
            options.headers = {};
          }
          if (options.headers instanceof Headers) {
            if (!options.headers.has('X-CSRF-Token')) {
              options.headers.set('X-CSRF-Token', csrfToken);
            }
          } else if (Array.isArray(options.headers)) {
            options.headers.push(['X-CSRF-Token', csrfToken]);
          } else {
            options.headers['X-CSRF-Token'] = options.headers['X-CSRF-Token'] || csrfToken;
          }
        }
      }
      return originalFetch(url, options);
    };

    document.querySelectorAll('form').forEach(form => {
      if ((form.method || '').toUpperCase() === 'POST' && !form.querySelector('input[name="csrf_token"]')) {
        const input = document.createElement('input');
        input.type = 'hidden';
        input.name = 'csrf_token';
        input.value = csrfToken;
        form.appendChild(input);
      }
    });
  }

  // Initialize Bootstrap Tooltips
  if (typeof bootstrap !== 'undefined') {
    const tooltipTriggerList = [].slice.call(document.querySelectorAll('[data-bs-toggle="tooltip"]'));
    tooltipTriggerList.map(function (tooltipTriggerEl) {
      return new bootstrap.Tooltip(tooltipTriggerEl);
    });
  }

  // Bulletproof Copy Function (Tested for Chrome & Safari on macOS/iOS over plain HTTP/Tailscale)
  function copyTextToClipboard(text) {
    if (!text) return Promise.reject(new Error("Empty text"));

    // 1. Try modern async Clipboard API if secure
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text).catch(() => {
        return execCommandFallback(text);
      });
    }

    // 2. Direct execCommand fallback for non-secure HTTP (Tailscale IP)
    return execCommandFallback(text);
  }

  function execCommandFallback(text) {
    return new Promise((resolve, reject) => {
      const el = document.createElement("textarea");
      el.value = text;
      el.setAttribute("readonly", "");
      el.style.position = "fixed";
      el.style.top = "0";
      el.style.left = "0";
      el.style.width = "2em";
      el.style.height = "2em";
      el.style.padding = "0";
      el.style.border = "none";
      el.style.outline = "none";
      el.style.boxShadow = "none";
      el.style.background = "transparent";
      el.style.opacity = "0.01";
      el.style.zIndex = "-1";

      document.body.appendChild(el);
      el.focus();
      el.select();
      el.setSelectionRange(0, el.value.length);

      try {
        const successful = document.execCommand("copy");
        document.body.removeChild(el);
        if (successful) {
          resolve(true);
        } else {
          reject(new Error("execCommand returned false"));
        }
      } catch (err) {
        document.body.removeChild(el);
        reject(err);
      }
    });
  }

  // Floating Toast Notification
  function showCopyToast(msg) {
    let container = document.getElementById("toast-container");
    if (!container) {
      container = document.createElement("div");
      container.id = "toast-container";
      container.style.cssText = "position: fixed; bottom: 24px; right: 24px; z-index: 99999; display: flex; flex-direction: column; gap: 8px;";
      document.body.appendChild(container);
    }

    const toast = document.createElement("div");
    toast.style.cssText = "background: #18181b; border: 1px solid rgba(16, 185, 129, 0.4); color: #fafafa; padding: 10px 16px; border-radius: 8px; font-size: 0.82rem; font-family: 'Geist', sans-serif; box-shadow: 0 10px 25px -5px rgba(0,0,0,0.5); display: flex; align-items: center; gap: 8px; animation: fadeIn 0.2s ease;";
    toast.innerHTML = `<i class="bi bi-check-circle-fill text-success"></i> <span>${msg || "Copied to clipboard!"}</span>`;
    
    container.appendChild(toast);
    setTimeout(() => {
      toast.style.opacity = "0";
      toast.style.transition = "opacity 0.3s ease";
      setTimeout(() => toast.remove(), 300);
    }, 2000);
  }

  // Global Delegated Copy Handler
  document.addEventListener("click", function (e) {
    const btn = e.target.closest(".copy-btn");
    if (!btn) return;

    e.preventDefault();
    let text = btn.getAttribute("data-copy");

    const targetSelector = btn.getAttribute("data-target");
    if (!text && targetSelector) {
      const targetEl = document.querySelector(targetSelector);
      if (targetEl) {
        text = targetEl.textContent.trim();
      }
    }

    if (!text) return;

    copyTextToClipboard(text).then(() => {
      const originalHtml = btn.innerHTML;
      btn.innerHTML = '<i class="bi bi-check2 text-success"></i> Copied!';
      btn.classList.add("text-success");
      
      showCopyToast("Copied to clipboard!");

      setTimeout(() => {
        btn.innerHTML = originalHtml;
        btn.classList.remove("text-success");
      }, 2000);
    }).catch(err => {
      console.warn("Fallback to prompt:", err);
      window.prompt("Copy to clipboard: Ctrl+C, Enter", text);
    });
  });

  // Dynamic Base URL adaptation to current browser origin
  const browserBaseURL = window.location.origin + "/v1";
  document.querySelectorAll(".current-base-url-text").forEach(el => {
    el.textContent = browserBaseURL;
  });

  // Sidebar Toggle for Mobile & Responsive Devices
  const sidebar = document.getElementById("appSidebar");
  const backdrop = document.getElementById("sidebarBackdrop");
  const btnToggle = document.getElementById("btnToggleSidebar");
  const btnClose = document.getElementById("btnCloseSidebar");

  function openSidebar() {
    if (sidebar) sidebar.classList.add("show");
    if (backdrop) backdrop.classList.add("show");
  }

  function closeSidebar() {
    if (sidebar) sidebar.classList.remove("show");
    if (backdrop) backdrop.classList.remove("show");
  }

  if (btnToggle) btnToggle.addEventListener("click", openSidebar);
  if (btnClose) btnClose.addEventListener("click", closeSidebar);
  if (backdrop) backdrop.addEventListener("click", closeSidebar);

  // Automatic Local Timezone Conversion for all .local-time elements
  function updateAllLocalTimes() {
    document.querySelectorAll(".local-time").forEach(el => {
      const utcStr = el.getAttribute("data-utc");
      if (!utcStr) return;

      let date;
      if (!utcStr.includes("Z") && !utcStr.includes("+")) {
        date = new Date(utcStr.replace(" ", "T") + "Z");
      } else {
        date = new Date(utcStr);
      }

      if (isNaN(date.getTime())) return;

      const yyyy = date.getFullYear();
      const mm = String(date.getMonth() + 1).padStart(2, '0');
      const dd = String(date.getDate()).padStart(2, '0');
      const hh = String(date.getHours()).padStart(2, '0');
      const min = String(date.getMinutes()).padStart(2, '0');
      const ss = String(date.getSeconds()).padStart(2, '0');

      el.textContent = `${yyyy}-${mm}-${dd} ${hh}:${min}:${ss}`;
      el.title = `UTC: ${utcStr}`;
    });
  }
  updateAllLocalTimes();

  // Show user's detected timezone in navbar badge
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    const tzBadge = document.getElementById("userTimezoneBadge");
    if (tzBadge) {
      tzBadge.textContent = tz;
      tzBadge.title = `Browser timezone: ${tz}`;
    }
  } catch (e) {}

  // Direct Click on Key Display
  document.addEventListener("click", function(e) {
    const keyDisplay = e.target.closest(".key-display");
    if (!keyDisplay) return;
    const fullKey = keyDisplay.getAttribute("data-full");
    if (fullKey) {
      copyTextToClipboard(fullKey).then(() => {
        showCopyToast("API Key copied to clipboard!");
      });
    }
  });

  // Auto-enhance flash messages containing API keys with a direct Copy button
  document.querySelectorAll(".alert").forEach(alert => {
    const text = alert.textContent;
    const match = text.match(/(sk-gw-[a-zA-Z0-9_-]+)/);
    if (match && match[1]) {
      const key = match[1];
      const copyBtn = document.createElement("button");
      copyBtn.type = "button";
      copyBtn.className = "btn-modern-ghost btn-sm py-1 px-2 copy-btn ms-2";
      copyBtn.setAttribute("data-copy", key);
      copyBtn.innerHTML = '<i class="bi bi-clipboard"></i> Copy Key';
      alert.querySelector("div")?.appendChild(copyBtn);
    }
  });
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", initApp);
} else {
  initApp();
}
