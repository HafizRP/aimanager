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

  // Helper for escaping HTML strings
  function escapeHtml(str) {
    if (!str) return "";
    return String(str)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#039;");
  }

  // Quick Menu Search & Command Palette (Ctrl+K)
  function initMenuSearch() {
    const sidebarNavContent = document.getElementById("sidebarNavContent") || document.querySelector(".sidebar-content");
    if (!sidebarNavContent) return;

    const sidebarLinks = Array.from(sidebarNavContent.querySelectorAll(".sidebar-link"));
    const navGroups = Array.from(sidebarNavContent.querySelectorAll(".sidebar-nav-group"));
    const sidebarSearchInput = document.getElementById("sidebarMenuSearch");
    const clearSidebarSearchBtn = document.getElementById("clearSidebarSearch");
    const sidebarSearchKbd = document.getElementById("sidebarSearchKbd");
    const sidebarEmptyMsg = document.getElementById("sidebarMenuEmpty");

    const commandPaletteModalEl = document.getElementById("commandPaletteModal");
    const commandPaletteInput = document.getElementById("commandPaletteInput");
    const commandPaletteResults = document.getElementById("commandPaletteResults");
    const commandPaletteCount = document.getElementById("commandPaletteCount");
    const btnOpenSearchMobile = document.getElementById("btnOpenSearchMobile");

    // Index all navigation items from sidebar
    const menuItems = sidebarLinks.map(link => {
      const titleEl = link.querySelector("span:not(.badge-modern)");
      const badgeEl = link.querySelector(".badge-modern");
      const iconEl = link.querySelector("i");
      const groupEl = link.closest(".sidebar-nav-group")?.querySelector(".sidebar-group-title");
      return {
        href: link.getAttribute("href") || "#",
        title: titleEl ? titleEl.textContent.trim() : link.textContent.trim(),
        group: groupEl ? groupEl.textContent.trim() : "Menu",
        iconClass: iconEl ? iconEl.className : "bi bi-link-45deg",
        badgeText: badgeEl ? badgeEl.textContent.trim() : "",
        keywords: (link.getAttribute("data-keywords") || "").toLowerCase(),
        el: link
      };
    });

    // 1. In-sidebar live filter
    function filterSidebar(query) {
      const q = query.trim().toLowerCase();
      let totalVisible = 0;

      if (!q) {
        sidebarLinks.forEach(link => { link.style.display = ""; });
        navGroups.forEach(group => {
          group.style.display = "";
          const title = group.querySelector(".sidebar-group-title");
          if (title) title.style.display = "";
        });
        if (clearSidebarSearchBtn) clearSidebarSearchBtn.classList.add("d-none");
        if (sidebarSearchKbd) sidebarSearchKbd.classList.remove("d-none");
        if (sidebarEmptyMsg) sidebarEmptyMsg.classList.add("d-none");
        return;
      }

      if (clearSidebarSearchBtn) clearSidebarSearchBtn.classList.remove("d-none");
      if (sidebarSearchKbd) sidebarSearchKbd.classList.add("d-none");

      navGroups.forEach(group => {
        let groupVisible = 0;
        const links = group.querySelectorAll(".sidebar-link");
        links.forEach(link => {
          const item = menuItems.find(m => m.el === link);
          const matchTitle = item && item.title.toLowerCase().includes(q);
          const matchKeywords = item && item.keywords.includes(q);
          const matchGroup = item && item.group.toLowerCase().includes(q);
          const matchHref = item && item.href.toLowerCase().includes(q);

          if (matchTitle || matchKeywords || matchGroup || matchHref) {
            link.style.display = "flex";
            groupVisible++;
            totalVisible++;
          } else {
            link.style.display = "none";
          }
        });

        const title = group.querySelector(".sidebar-group-title");
        if (groupVisible === 0) {
          group.style.display = "none";
        } else {
          group.style.display = "";
          if (title) title.style.display = "";
        }
      });

      if (sidebarEmptyMsg) {
        if (totalVisible === 0) {
          sidebarEmptyMsg.classList.remove("d-none");
        } else {
          sidebarEmptyMsg.classList.add("d-none");
        }
      }
    }

    if (sidebarSearchInput) {
      sidebarSearchInput.addEventListener("input", function() {
        filterSidebar(this.value);
      });

      sidebarSearchInput.addEventListener("keydown", function(e) {
        if (e.key === "Enter") {
          e.preventDefault();
          const firstVisible = sidebarLinks.find(link => link.style.display !== "none");
          if (firstVisible) {
            window.location.href = firstVisible.getAttribute("href");
          }
        } else if (e.key === "Escape") {
          sidebarSearchInput.value = "";
          filterSidebar("");
          sidebarSearchInput.blur();
        }
      });
    }

    if (clearSidebarSearchBtn) {
      clearSidebarSearchBtn.addEventListener("click", function() {
        if (sidebarSearchInput) {
          sidebarSearchInput.value = "";
          filterSidebar("");
          sidebarSearchInput.focus();
        }
      });
    }

    // 2. Command Palette Modal (Ctrl+K)
    let bsModal = null;
    if (commandPaletteModalEl && typeof bootstrap !== "undefined") {
      bsModal = bootstrap.Modal.getOrCreateInstance(commandPaletteModalEl);
    }

    let selectedIndex = 0;
    let currentResults = [];

    function renderPaletteResults(filtered) {
      currentResults = filtered;
      selectedIndex = 0;
      if (!commandPaletteResults) return;

      if (filtered.length === 0) {
        commandPaletteResults.innerHTML = `
          <div class="text-center py-4 px-2">
            <i class="bi bi-search text-dim fs-4 d-block mb-2"></i>
            <span class="text-dim small">No matching menu found</span>
          </div>`;
        if (commandPaletteCount) commandPaletteCount.textContent = "0 items";
        return;
      }

      if (commandPaletteCount) commandPaletteCount.textContent = `${filtered.length} item${filtered.length > 1 ? "s" : ""}`;

      commandPaletteResults.innerHTML = filtered.map((item, idx) => `
        <a href="${item.href}" class="command-palette-item ${idx === 0 ? 'active' : ''}" data-index="${idx}">
          <div class="command-palette-icon">
            <i class="${item.iconClass}"></i>
          </div>
          <div class="d-flex flex-column min-w-0 flex-grow-1">
            <div class="d-flex align-items-center gap-2">
              <span class="fw-medium text-light text-truncate" style="font-size:0.86rem;">${escapeHtml(item.title)}</span>
              ${item.badgeText ? `<span class="badge-modern badge-indigo" style="font-size:0.62rem; padding:0.05rem 0.35rem;">${escapeHtml(item.badgeText)}</span>` : ''}
            </div>
            <span class="text-dim text-truncate font-monospace" style="font-size:0.68rem;">${item.href} &middot; ${escapeHtml(item.group)}</span>
          </div>
          <i class="bi bi-arrow-return-left text-dim small ms-auto"></i>
        </a>
      `).join("");

      commandPaletteResults.querySelectorAll(".command-palette-item").forEach(el => {
        el.addEventListener("mouseenter", function() {
          const idx = parseInt(this.getAttribute("data-index"), 10);
          updateActiveResult(idx);
        });
      });
    }

    function updateActiveResult(newIndex) {
      if (!commandPaletteResults) return;
      const items = commandPaletteResults.querySelectorAll(".command-palette-item");
      if (items.length === 0) return;

      items.forEach(el => el.classList.remove("active"));
      selectedIndex = Math.max(0, Math.min(newIndex, items.length - 1));
      const activeEl = items[selectedIndex];
      if (activeEl) {
        activeEl.classList.add("active");
        activeEl.scrollIntoView({ block: "nearest" });
      }
    }

    function filterCommandPalette(query) {
      const q = query.trim().toLowerCase();
      if (!q) {
        renderPaletteResults(menuItems);
        return;
      }

      const scored = [];
      menuItems.forEach(item => {
        const titleLower = item.title.toLowerCase();
        const kwLower = item.keywords;
        const groupLower = item.group.toLowerCase();
        const hrefLower = item.href.toLowerCase();

        let score = 0;
        if (titleLower === q) score = 100;
        else if (titleLower.startsWith(q)) score = 80;
        else if (titleLower.includes(q)) score = 60;
        else if (kwLower.includes(q)) score = 40;
        else if (hrefLower.includes(q)) score = 30;
        else if (groupLower.includes(q)) score = 20;

        if (score > 0) {
          scored.push({ item, score });
        }
      });

      scored.sort((a, b) => b.score - a.score);
      renderPaletteResults(scored.map(s => s.item));
    }

    function openCommandPalette() {
      if (!bsModal) return;
      const sidebar = document.getElementById("appSidebar");
      const backdrop = document.getElementById("sidebarBackdrop");
      if (sidebar) sidebar.classList.remove("show");
      if (backdrop) backdrop.classList.remove("show");

      bsModal.show();
      if (commandPaletteInput) {
        commandPaletteInput.value = "";
        setTimeout(() => {
          commandPaletteInput.focus();
          filterCommandPalette("");
        }, 120);
      }
    }

    if (commandPaletteInput) {
      commandPaletteInput.addEventListener("input", function() {
        filterCommandPalette(this.value);
      });

      commandPaletteInput.addEventListener("keydown", function(e) {
        if (e.key === "ArrowDown") {
          e.preventDefault();
          updateActiveResult(selectedIndex + 1);
        } else if (e.key === "ArrowUp") {
          e.preventDefault();
          updateActiveResult(selectedIndex - 1);
        } else if (e.key === "Enter") {
          e.preventDefault();
          if (currentResults[selectedIndex]) {
            window.location.href = currentResults[selectedIndex].href;
          }
        }
      });
    }

    document.addEventListener("keydown", function(e) {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        openCommandPalette();
        return;
      }
      if (e.key === "/" && !["INPUT", "TEXTAREA", "SELECT"].includes(document.activeElement?.tagName) && !document.activeElement?.isContentEditable) {
        e.preventDefault();
        openCommandPalette();
        return;
      }
    });

    if (btnOpenSearchMobile) {
      btnOpenSearchMobile.addEventListener("click", openCommandPalette);
    }

    if (sidebarSearchKbd) {
      sidebarSearchKbd.addEventListener("click", function(e) {
        e.stopPropagation();
        openCommandPalette();
      });
    }
  }

  initMenuSearch();
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", initApp);
} else {
  initApp();
}
