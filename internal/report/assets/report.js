// Enhancements only. The report is complete and readable with scripts off:
// findings collapse with native <details>, and nothing here loads or sends
// anything.
(function () {
  "use strict";
  var doc = document;
  doc.documentElement.classList.add("js");

  var findings = Array.prototype.slice.call(doc.querySelectorAll("details.finding"));
  var categories = Array.prototype.slice.call(doc.querySelectorAll(".category"));
  var filters = Array.prototype.slice.call(doc.querySelectorAll("[data-filter]"));

  function applyFilter(severity) {
    findings.forEach(function (f) {
      f.hidden = severity !== "all" && f.getAttribute("data-severity") !== severity;
    });
    categories.forEach(function (c) {
      var all = c.querySelectorAll("details.finding");
      var shown = c.querySelectorAll("details.finding:not([hidden])");
      var note = c.querySelector(".no-match");
      if (note) {
        note.hidden = all.length === 0 || shown.length > 0;
      }
    });
    filters.forEach(function (b) {
      b.setAttribute("aria-pressed", String(b.getAttribute("data-filter") === severity));
    });
  }

  function setOpen(open) {
    findings.forEach(function (f) {
      if (!f.hidden) {
        f.open = open;
      }
    });
  }

  function copyText(text, button) {
    function done() {
      var label = button.textContent;
      button.textContent = "Copied";
      setTimeout(function () { button.textContent = label; }, 1500);
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, function () { fallbackCopy(text, done); });
    } else {
      fallbackCopy(text, done);
    }
  }

  function fallbackCopy(text, done) {
    var area = doc.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.opacity = "0";
    doc.body.appendChild(area);
    area.select();
    try {
      if (doc.execCommand("copy")) {
        done();
      }
    } catch (e) { /* copying is a convenience; ignore */ }
    doc.body.removeChild(area);
  }

  doc.addEventListener("click", function (event) {
    var el = event.target.closest ? event.target.closest("[data-filter], [data-action]") : null;
    if (!el) {
      return;
    }
    if (el.hasAttribute("data-filter")) {
      applyFilter(el.getAttribute("data-filter"));
      return;
    }
    switch (el.getAttribute("data-action")) {
      case "expand": setOpen(true); break;
      case "collapse": setOpen(false); break;
      case "copy":
        var pre = el.parentNode.querySelector("pre");
        if (pre) {
          copyText(pre.textContent, el);
        }
        break;
    }
  });

  // Following a link from the priorities list opens the finding it points at.
  function openTarget() {
    var id = decodeURIComponent(location.hash.slice(1));
    var target = id ? doc.getElementById(id) : null;
    if (target && target.tagName === "DETAILS") {
      if (target.hidden) {
        applyFilter("all");
      }
      target.open = true;
      target.scrollIntoView();
    }
  }
  window.addEventListener("hashchange", openTarget);
  openTarget();

  // Print everything, then put the page back the way it was.
  var reopen = [];
  window.addEventListener("beforeprint", function () {
    reopen = Array.prototype.slice.call(doc.querySelectorAll("details:not([open])"));
    reopen.forEach(function (d) { d.open = true; });
  });
  window.addEventListener("afterprint", function () {
    reopen.forEach(function (d) { d.open = false; });
    reopen = [];
  });
})();
