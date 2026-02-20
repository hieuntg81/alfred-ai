package tool

import "fmt"

// maxContentChars is the maximum number of characters returned by the content extraction JS.
const maxContentChars = 50000

// contentExtractionJS returns a JavaScript snippet that extracts AI-friendly
// structured content from the given DOM target (e.g. "document.body" or
// "document.querySelector('#main')"). The snippet returns a JSON string with
// title, url, text (readable content with CSS selectors), links, and forms.
func contentExtractionJS(target string) string {
	return fmt.Sprintf(`(function() {
  var MAX = %d;
  var root = %s;
  if (!root) return JSON.stringify({title: document.title, url: location.href, text: "[no content]", links: [], forms: []});

  var out = [];
  var links = [];
  var forms = [];
  var linkIdx = 0;
  var formIdx = 0;
  var len = 0;
  var truncated = false;

  function cssSelector(el) {
    if (el.id) return '#' + CSS.escape(el.id);
    var path = [];
    while (el && el.nodeType === 1) {
      var sel = el.tagName.toLowerCase();
      if (el.id) { path.unshift('#' + CSS.escape(el.id)); break; }
      var sib = el, nth = 1;
      while ((sib = sib.previousElementSibling)) { if (sib.tagName === el.tagName) nth++; }
      var count = 0;
      sib = el;
      while ((sib = sib.nextElementSibling)) { if (sib.tagName === el.tagName) count++; }
      if (nth > 1 || count > 0) sel += ':nth-of-type(' + nth + ')';
      path.unshift(sel);
      el = el.parentElement;
    }
    return path.join(' > ');
  }

  function emit(s) {
    if (truncated) return;
    if (len + s.length > MAX) { out.push('[truncated]'); truncated = true; return; }
    out.push(s);
    len += s.length;
  }

  function walk(node) {
    if (truncated) return;
    if (node.nodeType === 3) {
      var t = node.textContent.trim();
      if (t) emit(t);
      return;
    }
    if (node.nodeType !== 1) return;
    var tag = node.tagName.toLowerCase();

    // Skip hidden elements, scripts, styles
    if (tag === 'script' || tag === 'style' || tag === 'noscript' || tag === 'svg') return;
    var st = window.getComputedStyle(node);
    if (st.display === 'none' || st.visibility === 'hidden') return;

    // Headings
    if (/^h[1-6]$/.test(tag)) {
      emit('\n[' + tag + '] ' + node.textContent.trim());
      return;
    }

    // Links
    if (tag === 'a' && node.href) {
      var sel = cssSelector(node);
      var text = node.textContent.trim();
      links.push({index: linkIdx, text: text, href: node.href, selector: sel});
      emit('[link selector="' + sel + '" href="' + node.href + '"] ' + text);
      linkIdx++;
      return;
    }

    // Buttons
    if (tag === 'button' || (tag === 'input' && (node.type === 'submit' || node.type === 'button'))) {
      var sel = cssSelector(node);
      var label = node.textContent.trim() || node.value || node.getAttribute('aria-label') || '';
      emit('[button selector="' + sel + '"] ' + label);
      return;
    }

    // Form inputs
    if (tag === 'input' || tag === 'textarea' || tag === 'select') {
      var sel = cssSelector(node);
      var inputType = node.type || 'text';
      var name = node.name || '';
      var ph = node.placeholder || '';
      emit('[input selector="' + sel + '" type="' + inputType + '" name="' + name + '"' + (ph ? ' placeholder="' + ph + '"' : '') + ']');
      return;
    }

    // Forms
    if (tag === 'form') {
      var sel = cssSelector(node);
      var action = node.action || '';
      var fields = [];
      var inputs = node.querySelectorAll('input, textarea, select');
      for (var i = 0; i < inputs.length; i++) {
        var inp = inputs[i];
        fields.push({
          name: inp.name || '',
          type: inp.type || 'text',
          placeholder: inp.placeholder || '',
          selector: cssSelector(inp)
        });
      }
      forms.push({index: formIdx, action: action, fields: fields});
      emit('\n[form selector="' + sel + '" action="' + action + '"]');
      formIdx++;
    }

    // Block-level elements get newlines
    if (/^(div|p|section|article|main|header|footer|nav|ul|ol|li|blockquote|pre|table|tr|td|th|dl|dt|dd|figure|figcaption)$/.test(tag)) {
      emit('\n');
    }

    // Recurse into children
    for (var i = 0; i < node.childNodes.length; i++) {
      walk(node.childNodes[i]);
    }

    if (/^(div|p|section|article|li|blockquote|tr)$/.test(tag)) {
      emit('\n');
    }
  }

  walk(root);

  var text = out.join(' ').replace(/[ \t]+/g, ' ').replace(/\n\s*\n/g, '\n\n').trim();

  return JSON.stringify({
    title: document.title,
    url: location.href,
    text: text,
    links: links,
    forms: forms
  });
})()`, maxContentChars, target)
}
