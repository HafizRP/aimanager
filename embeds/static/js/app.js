document.addEventListener("DOMContentLoaded", function () {
  // Initialize Bootstrap Tooltips
  const tooltipTriggerList = [].slice.call(document.querySelectorAll('[data-bs-toggle="tooltip"]'));
  tooltipTriggerList.map(function (tooltipTriggerEl) {
    return new bootstrap.Tooltip(tooltipTriggerEl);
  });

  // Robust Copy Function with HTTP/Tailscale Insecure Context Fallback
  function copyTextToClipboard(text) {
    return new Promise((resolve, reject) => {
      // 1. Modern API (only works in HTTPS or localhost)
      if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text)
          .then(() => resolve(true))
          .catch(() => fallbackCopy(text, resolve, reject));
      } else {
        // 2. Fallback for HTTP over Tailscale IP
        fallbackCopy(text, resolve, reject);
      }
    });
  }

  function fallbackCopy(text, resolve, reject) {
    try {
      const textArea = document.createElement("textarea");
      textArea.value = text;
      textArea.style.position = "fixed";
      textArea.style.left = "-9999px";
      textArea.style.top = "-9999px";
      textArea.setAttribute("readonly", "");
      document.body.appendChild(textArea);
      
      textArea.focus();
      textArea.select();
      textArea.setSelectionRange(0, 99999); // For mobile devices

      const successful = document.execCommand("copy");
      document.body.removeChild(textArea);

      if (successful) {
        resolve(true);
      } else {
        reject(new Error("document.execCommand failed"));
      }
    } catch (err) {
      reject(err);
    }
  }

  // Toast Notification Helper
  function showCopyToast(msg) {
    let container = document.getElementById("toast-container");
    if (!container) {
      container = document.createElement("div");
      container.id = "toast-container";
      container.style.cssText = "position: fixed; bottom: 24px; right: 24px; z-index: 99999; display: flex; flex-direction: column; gap: 8px;";
      document.body.appendChild(container);
    }

    const toast = document.createElement("div");
    toast.style.cssText = "background: #161b2a; border: 1px solid rgba(16, 185, 129, 0.4); color: #f8fafc; padding: 10px 16px; border-radius: 8px; font-size: 0.82rem; font-family: Inter, sans-serif; box-shadow: 0 10px 25px -5px rgba(0,0,0,0.5); display: flex; align-items: center; gap: 8px; animation: fadeIn 0.2s ease;";
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
    const text = btn.getAttribute("data-copy");
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
      console.error("Failed to copy:", err);
      window.prompt("Copy to clipboard: Ctrl+C, Enter", text);
    });
  });

  // Direct Click on Key Code element
  document.addEventListener("click", function(e) {
    const keyDisplay = e.target.closest(".key-display");
    if (!keyDisplay) return;
    const fullKey = keyDisplay.getAttribute("data-full");
    if (fullKey) {
      copyTextToClipboard(fullKey).then(() => {
        showCopyToast("API Key copied to clipboard!");
      }).catch(() => {
        window.prompt("Copy to clipboard: Ctrl+C, Enter", fullKey);
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
