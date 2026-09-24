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
    const sidebarSearchTrigger = document.getElementById("sidebarSearchTrigger");
    const sidebarEmptyMsg = document.getElementById("sidebarMenuEmpty");

    const commandPaletteModalEl = document.getElementById("commandPaletteModal");
    const commandPaletteInput = document.getElementById("commandPaletteInput");
    const commandPaletteResults = document.getElementById("commandPaletteResults");
    const commandPaletteCount = document.getElementById("commandPaletteCount");
    const btnOpenSearchMobile = document.getElementById("btnOpenSearchMobile");

    // macOS shortcut indicator
    const isMac = typeof navigator !== "undefined" && (navigator.platform.toUpperCase().indexOf('MAC') >= 0 || navigator.userAgent.toUpperCase().indexOf('MAC') >= 0);
    if (sidebarSearchKbd && isMac) {
      sidebarSearchKbd.textContent = "⌘K";
    }

    // Index all navigation items from sidebar
    const dropdownToggles = Array.from(sidebarNavContent.querySelectorAll(".sidebar-dropdown-toggle"));
    const collapseContainers = Array.from(sidebarNavContent.querySelectorAll(".collapse"));

    // Track initial collapse states (which submenus were open on page load)
    const initialCollapseMap = new Map();
    dropdownToggles.forEach(toggle => {
      const targetId = toggle.getAttribute("data-bs-target");
      const collapseEl = targetId ? sidebarNavContent.querySelector(targetId) : null;
      if (collapseEl) {
        initialCollapseMap.set(targetId, collapseEl.classList.contains("show"));
      }
    });

    const menuItems = sidebarLinks.map(link => {
      const titleEl = link.querySelector("span:not(.badge-modern)");
      const badgeEl = link.querySelector(".badge-modern");
      const iconEl = link.querySelector("i");
      // Find parent sub-menu toggle or group title
      const collapseParent = link.closest(".collapse");
      const dropdownToggle = collapseParent ? sidebarNavContent.querySelector(`.sidebar-dropdown-toggle[data-bs-target="#${collapseParent.id}"]`) : null;
      const subGroupTitle = dropdownToggle ? dropdownToggle.querySelector("span")?.textContent.trim() : null;
      const groupEl = link.closest(".sidebar-nav-group")?.querySelector(".sidebar-group-title");
      const groupName = subGroupTitle || (groupEl ? groupEl.textContent.trim() : "Menu");

      return {
        href: link.getAttribute("href") || "#",
        title: titleEl ? titleEl.textContent.trim() : link.textContent.trim(),
        group: groupName,
        iconClass: iconEl ? iconEl.className : "bi bi-link-45deg",
        badgeText: badgeEl ? badgeEl.textContent.trim() : "Menu",
        keywords: (link.getAttribute("data-keywords") || "").toLowerCase(),
        el: link,
        collapseParent: collapseParent
      };
    });

    // Detect if user has admin view
    const isAdmin = Boolean(sidebarNavContent.querySelector('a[href="/users"]') || sidebarNavContent.querySelector('a[href="/models"]') || sidebarNavContent.querySelector('a[href="/settings"]'));

    const allSearchItems = [...menuItems];

    // Quick Actions and Direct Settings Jumps
    const quickItems = [
      {
        title: "Toggle UI Theme (Dark / Light Mode)",
        group: "Quick Action",
        href: "",
        iconClass: "bi bi-moon-stars text-cyan",
        badgeText: "Action",
        keywords: "theme toggle dark light mode tema gelap terang ubah warna switch display tampilan mode",
        action: function() {
          const btn = document.getElementById("themeToggle") || document.getElementById("themeToggleMobile");
          if (btn) btn.click();
        }
      },
      {
        title: "Ganti Password / Security Credentials",
        group: "Account Settings",
        href: "/profile",
        iconClass: "bi bi-shield-lock text-warning",
        badgeText: "Setting",
        keywords: "password ganti ubah sandi kata sandi security credentials profile keamanan akun user pengaturan akun"
      },
      {
        title: "Create New API Key (Buat Kunci API)",
        group: "Access & Billing",
        href: "/keys",
        iconClass: "bi bi-key-fill text-warning",
        badgeText: "Action",
        keywords: "api key create new buat kunci sk-gw secret token budget generate baru"
      }
    ];

    if (isAdmin) {
      quickItems.push(
        {
          title: "Gateway Upstream & SQLite Database Settings",
          group: "Gateway Settings",
          href: "/settings",
          iconClass: "bi bi-gear-fill text-muted",
          badgeText: "Setting",
          keywords: "upstream url database path core sqlite settings sync port 20128 gateway konfigurasi pengaturan core database"
        },
        {
          title: "Midtrans Payment Gateway Configuration",
          group: "Payment Settings",
          href: "/settings",
          iconClass: "bi bi-credit-card text-emerald",
          badgeText: "Setting",
          keywords: "midtrans server key client key production sandbox qris gopay va payment gateway pembayaran pengaturan midtrans"
        },
        {
          title: "Token Saver: RTK Compression & Thinking Modes",
          group: "Optimization Settings",
          href: "/token-saver",
          iconClass: "bi bi-magic text-emerald",
          badgeText: "Setting",
          keywords: "rtk compression thinking intensity antigravity opencode mode ponytail caveman penghemat token pengaturan kompresi"
        },
        {
          title: "Setup CLI Tools (Cursor, Claude, Codex, Hermes)",
          group: "CLI Hub",
          href: "/cli-tools",
          iconClass: "bi bi-tools text-cyan",
          badgeText: "Tools",
          keywords: "cli tools setup cursor claude codex cline hermes copilot opencode panduan konfigurasi terminal ide"
        }
      );
    }

    // Direct subpage links for command palette
    const hubSubpages = [
      // Developer Hub
      { title: "Endpoint Hub (URLs & Loopback)", group: "Developer Hub", href: "/endpoint", iconClass: "bi bi-plug-fill text-info", badgeText: "Endpoint", keywords: "endpoint urls api loopback proxy base completion port" },
      { title: "API Keys (Secret & Budgets)", group: "Developer Hub", href: "/keys", iconClass: "bi bi-key text-warning", badgeText: "Auth", keywords: "api keys secret token budget rate limit rotate revoke kunci api" },
      { title: "CLI Tools Hub (Cursor, Claude, Codex, Hermes)", group: "Developer Hub", href: "/cli-tools", iconClass: "bi bi-tools text-cyan", badgeText: "Tools", keywords: "cli tools setup cursor claude codex cline hermes copilot opencode panduan konfigurasi terminal ide" },
      { title: "Agent Skills (Prompts & System Instructions)", group: "Developer Hub", href: "/skills", iconClass: "bi bi-puzzle-fill text-emerald", badgeText: "Skills", keywords: "agent skills prompt snippets system instructions templates universal keahlian agen" },

      // Observability & Stats
      { title: "Request Logs (Audit Traffic & Errors)", group: "Observability", href: "/logs", iconClass: "bi bi-activity text-info", badgeText: "Logs", keywords: "request logs audit traffic history status errors export csv json filter riwayat log" },
      { title: "Usage Analytics (Cost & Token Breakdown)", group: "Observability", href: "/usage", iconClass: "bi bi-bar-chart-fill text-primary", badgeText: "Usage", keywords: "usage analytics breakdown tokens provider model account cost pemakaian penggunaan statistik" },
      { title: "Cache Analytics & FinOps (Exact Response Hits)", group: "Observability", href: "/cache-analytics", iconClass: "bi bi-database-check text-cyan", badgeText: "FinOps", keywords: "cache analytics exact response hit ratio sha256 savings finops rtk analisis cache" },
      { title: "Speed Benchmark (TTFT & Throughput)", group: "Observability", href: "/benchmark", iconClass: "bi bi-speedometer text-warning", badgeText: "Speed", keywords: "speed benchmark ttft latency performance throughput test leaderboard uji kecepatan" },
      { title: "Replay Lab (Prompt Diff & Model Compare)", group: "Observability", href: "/replay", iconClass: "bi bi-repeat text-info", badgeText: "Lab", keywords: "replay lab prompt diff compare models side by side output latency rerun uji ulang" },
      { title: "Anomaly Radar (Error Spikes & Baseline Cutoff)", group: "Observability", href: "/radar", iconClass: "bi bi-radar text-rose", badgeText: "Radar", keywords: "anomaly radar security spike alerts errors baseline detection cutoff radar anomali" },

      // Engine & Admin
      { title: "Models & Aliases (Catalog & Context)", group: "Models & Routing", href: "/models", iconClass: "bi bi-cpu text-primary", badgeText: "Models", keywords: "models aliases upstream mapping context list gpt claude gemini daftar model alias" },
      { title: "Model Combos (Fallback & Failover Chains)", group: "Models & Routing", href: "/combos", iconClass: "bi bi-diagram-3 text-indigo", badgeText: "Combos", keywords: "model combos fallback chains failover round robin fusion strategy sticky kombo model" },
      { title: "Pricing Catalog (Rates per Million)", group: "Models & Routing", href: "/pricing", iconClass: "bi bi-tag text-warning", badgeText: "Pricing", keywords: "pricing token rates cost per million upstream catalog harga tarif biaya katalog" },
      { title: "Protocol Translator (OpenAI / Anthropic / Gemini)", group: "Models & Routing", href: "/translator", iconClass: "bi bi-translate text-emerald", badgeText: "Translator", keywords: "translator protocol format translation convert anthropic openai gemini penerjemah" },

      { title: "Upstream Providers (Accounts & OAuth)", group: "Providers & Networks", href: "/providers", iconClass: "bi bi-cloud-check text-primary", badgeText: "Providers", keywords: "providers connections upstream antigravity kiro openai oauth accounts priority test penyedia akun" },
      { title: "Provider Nodes (Self-hosted Endpoints)", group: "Providers & Networks", href: "/nodes", iconClass: "bi bi-server text-cyan", badgeText: "Nodes", keywords: "provider nodes openai compatible self hosted endpoints prefix routing node penyedia" },
      { title: "Quota Overview (Rolling Windows & Reset Timers)", group: "Providers & Networks", href: "/quota", iconClass: "bi bi-pie-chart-fill text-emerald", badgeText: "Quota", keywords: "quota overview rolling windows accounts reset timers limits pools ringkasan kuota" },
      { title: "Proxy Pools (Outbound IP Rotation)", group: "Providers & Networks", href: "/proxy-pools", iconClass: "bi bi-shield-shaded text-cyan", badgeText: "Proxies", keywords: "proxy pools egress outbound ip rotate socks5 http residential kolam proxy" },

      { title: "Token Saver & RTK (Compression & Modes)", group: "Optimization & Plugins", href: "/token-saver", iconClass: "bi bi-magic text-emerald", badgeText: "Saver", keywords: "token saver rtk compression thinking intensity ponytail caveman headroom pxpipe penghemat" },
      { title: "PXPipe (Prompt Transform & Image Compression)", group: "Optimization & Plugins", href: "/pxpipe", iconClass: "bi bi-funnel text-info", badgeText: "PXPipe", keywords: "pxpipe prompt transform images compression pipeline proxy pipa transformasi" },
      { title: "Media Voices (TTS Audio Synthesis)", group: "Optimization & Plugins", href: "/media", iconClass: "bi bi-speaker text-pink", badgeText: "TTS", keywords: "media voices tts audio speech deepgram elevenlabs minimax voices suara media" },
      { title: "MITM Bridge (Antigravity DNS Interception)", group: "Optimization & Plugins", href: "/mitm", iconClass: "bi bi-shuffle text-warning", badgeText: "MITM", keywords: "mitm bridge antigravity dns interception proxy certificate tools jembatan mitm" },
      { title: "MCP Inspector (Model Context Protocol)", group: "Optimization & Plugins", href: "/mcp", iconClass: "bi bi-boxes text-emerald", badgeText: "MCP", keywords: "mcp inspector model context protocol cowork tools registry inspektur mcp" },

      { title: "Users & Quotas (Accounts & Roles)", group: "System & Settings", href: "/users", iconClass: "bi bi-people-fill text-warning", badgeText: "Users", keywords: "users quotas management accounts role admin active daily reset password pengguna" },
      { title: "Gateway Settings (Config & Secrets)", group: "System & Settings", href: "/settings", iconClass: "bi bi-gear-fill text-muted", badgeText: "Config", keywords: "gateway settings upstream core midtrans configuration secrets env pengaturan gateway" },
      { title: "Console Log (Live Engine Stream)", group: "System & Settings", href: "/console-log", iconClass: "bi bi-terminal-split text-info", badgeText: "Console", keywords: "console log server core translator live stream debug output log konsol" },

      { title: "My Profile & Security Settings", group: "Account", href: "/profile", iconClass: "bi bi-person-circle text-muted", badgeText: "Profile", keywords: "profile user account password credentials timezone quota profil saya akun" },
      { title: "Buy Tokens (Topup & Invoice)", group: "Account", href: "/billing", iconClass: "bi bi-cart text-emerald", badgeText: "Billing", keywords: "billing buy tokens topup payment midtrans qris gopay va credit invoice balance beli token" }
    ];

    hubSubpages.forEach(item => {
      if (!allSearchItems.some(x => x.href === item.href)) {
        allSearchItems.push(item);
      }
    });

    quickItems.forEach(item => allSearchItems.push(item));

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

        // Restore dropdown toggles and initial collapse state
        dropdownToggles.forEach(toggle => {
          toggle.style.display = "";
          const targetId = toggle.getAttribute("data-bs-target");
          const collapseEl = targetId ? sidebarNavContent.querySelector(targetId) : null;
          if (collapseEl) {
            const wasOpen = initialCollapseMap.get(targetId);
            if (wasOpen) {
              collapseEl.classList.add("show");
              toggle.classList.remove("collapsed");
              toggle.setAttribute("aria-expanded", "true");
            } else {
              collapseEl.classList.remove("show");
              toggle.classList.add("collapsed");
              toggle.setAttribute("aria-expanded", "false");
            }
          }
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

      // Manage dropdown collapse visibility: auto-expand if any child matches
      dropdownToggles.forEach(toggle => {
        const targetId = toggle.getAttribute("data-bs-target");
        const collapseEl = targetId ? sidebarNavContent.querySelector(targetId) : null;
        if (!collapseEl) return;

        const sublinks = Array.from(collapseEl.querySelectorAll(".sidebar-link"));
        const hasVisible = sublinks.some(link => link.style.display !== "none");

        if (hasVisible) {
          toggle.style.display = "flex";
          collapseEl.classList.add("show");
          toggle.classList.remove("collapsed");
          toggle.setAttribute("aria-expanded", "true");
        } else {
          toggle.style.display = "none";
          collapseEl.classList.remove("show");
          toggle.classList.add("collapsed");
          toggle.setAttribute("aria-expanded", "false");
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

    if (sidebarSearchTrigger) {
      sidebarSearchTrigger.addEventListener("click", function(e) {
        e.preventDefault();
        openCommandPalette();
      });
    }

    if (sidebarSearchInput) {
      sidebarSearchInput.addEventListener("click", function(e) {
        e.preventDefault();
        openCommandPalette();
      });
      sidebarSearchInput.addEventListener("focus", function(e) {
        e.preventDefault();
        this.blur();
        openCommandPalette();
      });
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
        } else if (e.key !== "Tab") {
          openCommandPalette();
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

    function executeItem(item) {
      if (!item) return;
      if (bsModal) bsModal.hide();
      if (typeof item.action === "function") {
        try {
          item.action();
        } catch (err) {
          console.error("Command palette action error:", err);
        }
      } else if (item.href && item.href !== "#") {
        window.location.href = item.href;
      }
    }

    function renderPaletteResults(filtered) {
      currentResults = filtered;
      selectedIndex = 0;
      if (!commandPaletteResults) return;

      if (filtered.length === 0) {
        commandPaletteResults.innerHTML = `
          <div class="text-center py-4 px-2">
            <i class="bi bi-search text-dim fs-4 d-block mb-2"></i>
            <span class="text-dim small d-block mb-1">Menu atau pengaturan tidak ditemukan</span>
            <span class="text-muted" style="font-size:0.75rem;">Coba kata kunci: <span class="text-cyan">pengaturan</span>, <span class="text-cyan">keys</span>, <span class="text-cyan">billing</span>, <span class="text-cyan">chat</span>, <span class="text-cyan">tema</span></span>
          </div>`;
        if (commandPaletteCount) commandPaletteCount.textContent = "0 items";
        return;
      }

      if (commandPaletteCount) commandPaletteCount.textContent = `${filtered.length} item${filtered.length > 1 ? "s" : ""}`;

      commandPaletteResults.innerHTML = filtered.map((item, idx) => {
        const badgeText = item.badgeText || (item.action ? "Action" : (item.href && item.href.startsWith("/settings") ? "Setting" : "Menu"));
        const badgeClass = badgeText === "Action" ? "badge-emerald" : (badgeText === "Setting" ? "badge-amber" : "badge-indigo");
        const displayPath = item.href ? item.href : (item.group || "Action");

        return `
        <a href="${item.href || '#'}" class="command-palette-item ${idx === 0 ? 'active' : ''}" data-index="${idx}">
          <div class="command-palette-icon">
            <i class="${item.iconClass}"></i>
          </div>
          <div class="d-flex flex-column min-w-0 flex-grow-1">
            <div class="d-flex align-items-center gap-2">
              <span class="fw-medium text-light text-truncate" style="font-size:0.86rem;">${escapeHtml(item.title)}</span>
              <span class="badge-modern ${badgeClass}" style="font-size:0.62rem; padding:0.05rem 0.35rem;">${escapeHtml(badgeText)}</span>
            </div>
            <span class="text-dim text-truncate font-monospace" style="font-size:0.68rem;">${escapeHtml(displayPath)} &middot; ${escapeHtml(item.group)}</span>
          </div>
          <i class="bi bi-arrow-return-left text-dim small ms-auto"></i>
        </a>`;
      }).join("");

      commandPaletteResults.querySelectorAll(".command-palette-item").forEach(el => {
        el.addEventListener("mouseenter", function() {
          const idx = parseInt(this.getAttribute("data-index"), 10);
          updateActiveResult(idx);
        });
        el.addEventListener("click", function(e) {
          e.preventDefault();
          const idx = parseInt(this.getAttribute("data-index"), 10);
          executeItem(currentResults[idx]);
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
        renderPaletteResults(allSearchItems);
        return;
      }

      const terms = q.split(/\s+/).filter(Boolean);
      const scored = [];

      allSearchItems.forEach(item => {
        const titleLower = item.title.toLowerCase();
        const kwLower = (item.keywords || "").toLowerCase();
        const groupLower = (item.group || "").toLowerCase();
        const hrefLower = (item.href || "").toLowerCase();
        const combined = `${titleLower} ${kwLower} ${groupLower} ${hrefLower}`;

        // All terms must match somewhere in the item
        const matchesAll = terms.every(term => combined.includes(term));
        if (!matchesAll) return;

        let score = 0;
        if (titleLower === q) score += 120;
        else if (titleLower.startsWith(q)) score += 90;
        else if (titleLower.includes(q)) score += 60;

        terms.forEach(term => {
          if (titleLower.startsWith(term)) score += 40;
          else if (titleLower.includes(term)) score += 25;
          if (kwLower.includes(term)) score += 20;
          if (groupLower.includes(term)) score += 15;
          if (hrefLower.includes(term)) score += 10;
        });

        scored.push({ item, score });
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
            executeItem(currentResults[selectedIndex]);
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
