document.addEventListener("DOMContentLoaded", function () {
  // Initialize Bootstrap Tooltips
  const tooltipTriggerList = [].slice.call(document.querySelectorAll('[data-bs-toggle="tooltip"]'));
  tooltipTriggerList.map(function (tooltipTriggerEl) {
    return new bootstrap.Tooltip(tooltipTriggerEl);
  });

  // Global Copy to Clipboard Handler
  document.querySelectorAll(".copy-btn").forEach(function (btn) {
    btn.addEventListener("click", function (e) {
      e.preventDefault();
      const text = this.getAttribute("data-copy");
      if (!text) return;

      navigator.clipboard.writeText(text).then(() => {
        const originalHtml = this.innerHTML;
        this.innerHTML = '<i class="bi bi-check2 text-success"></i> Copied!';
        setTimeout(() => {
          this.innerHTML = originalHtml;
        }, 2000);
      }).catch(err => {
        console.error("Failed to copy:", err);
      });
    });
  });
});
