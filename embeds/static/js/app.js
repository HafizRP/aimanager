document.addEventListener("DOMContentLoaded", function () {
  // Initialize Bootstrap Tooltips
  const tooltipTriggerList = [].slice.call(document.querySelectorAll('[data-bs-toggle="tooltip"]'));
  tooltipTriggerList.map(function (tooltipTriggerEl) {
    return new bootstrap.Tooltip(tooltipTriggerEl);
  });

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
      // Must be inside viewport and not hidden for Safari/WebKit security rules
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
      
      // Selection handling across mobile & desktop
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
    toast.style.cssText = "background: #161b2a; border: 1px solid rgba(16, 185, 129, 0.4); color: #f8fafc; padding: 10px 16px; border-radius: 8px; font-size: 0.82rem; font-family: Inter, sans-serif; box-shadow: 0 10px 25px -5px rgba(0,0,0,0.5); display: flex; align-items: center; gap: 8px;";
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

    // If data-target is specified (e.g. data-target="#codeBlock")
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
});
