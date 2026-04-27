'use strict';
const { parseToolCalls } = require('./parse');

// XML wrapper tag pair used by the streaming sieve.
const XML_TOOL_TAG_PAIRS = [
  { open: '<|dsml|tool_calls', close: '</|dsml|tool_calls>' },
  { open: '<dsml|tool_calls', close: '</dsml|tool_calls>' },
  { open: '<｜tool_calls', close: '</｜tool_calls>' },
  { open: '<|tool_calls', close: '</|tool_calls>' },
  { open: '<tool_calls', close: '</tool_calls>' },
];

const XML_TOOL_OPENING_TAGS = [
  ...XML_TOOL_TAG_PAIRS.map(p => p.open),
  '<|dsml|invoke', '<dsml|invoke', '<｜invoke', '<|invoke', '<invoke',
];

function consumeXMLToolCapture(captured, toolNames, trimWrappingJSONFence) {
  const lower = captured.toLowerCase();
  let anyOpenFound = false;
  let best = null;
  let rejected = null;

  // Scan every wrapper occurrence. Prose can mention a wrapper tag before the
  // actual tool block, including the same variant as the real block.
  for (const pair of XML_TOOL_TAG_PAIRS) {
    let searchFrom = 0;
    while (searchFrom < lower.length) {
      const openIdx = findXMLOpenOutsideCDATA(captured, pair.open, searchFrom);
      if (openIdx < 0) {
        break;
      }
      // Ignore closing tags that appear inside CDATA payloads, such as
      // write-file content containing tool-call documentation examples.
      const closeIdx = findMatchingXMLToolWrapperClose(captured, pair.open, pair.close, openIdx);
      if (closeIdx < 0) {
        anyOpenFound = true;
        searchFrom = openIdx + pair.open.length;
        continue;
      }
      const closeEnd = closeIdx + pair.close.length;
      const xmlBlock = captured.slice(openIdx, closeEnd);
      let prefixPart = captured.slice(0, openIdx);
      let suffixPart = captured.slice(closeEnd);
      const parsed = parseToolCalls(xmlBlock, toolNames);
      if (Array.isArray(parsed) && parsed.length > 0) {
        const trimmedFence = trimWrappingJSONFence(prefixPart, suffixPart);
        if (!best || openIdx < best.start) {
          best = {
            start: openIdx,
            prefix: trimmedFence.prefix,
            calls: parsed,
            suffix: trimmedFence.suffix,
          };
        }
        break;
      }
      if (!rejected || openIdx < rejected.start) {
        rejected = {
          start: openIdx,
          prefix: prefixPart + xmlBlock,
          suffix: suffixPart,
        };
      }
      searchFrom = openIdx + pair.open.length;
    }
  }
  if (best) {
    return { ready: true, prefix: best.prefix, calls: best.calls, suffix: best.suffix };
  }
  if (anyOpenFound) {
    // At least one opening tag was found but none had a matching close tag.
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  if (rejected) {
    // If this block failed to become a tool call, pass it through as text.
    return { ready: true, prefix: rejected.prefix, calls: [], suffix: rejected.suffix };
  }
  if (!containsAnyToolCallWrapper(lower)) {
    const found = firstInvokeIndex(lower);
    if (found.index >= 0) {
      const closeTag = found.dsml ? '</|dsml|tool_calls>' : '</tool_calls>';
      const openWrapper = found.dsml ? '<|DSML|tool_calls>' : '<tool_calls>';
      const closeIdx = findXMLCloseOutsideCDATA(captured, closeTag, found.index);
      if (closeIdx > found.index) {
        const closeEnd = closeIdx + closeTag.length;
        const xmlBlock = openWrapper + captured.slice(found.index, closeIdx) + closeTag;
        let prefixPart = captured.slice(0, found.index);
        let suffixPart = captured.slice(closeEnd);
        const parsed = parseToolCalls(xmlBlock, toolNames);
        if (Array.isArray(parsed) && parsed.length > 0) {
          const trimmedFence = trimWrappingJSONFence(prefixPart, suffixPart);
          return {
            ready: true,
            prefix: trimmedFence.prefix,
            calls: parsed,
            suffix: trimmedFence.suffix,
          };
        }
        return { ready: true, prefix: prefixPart + captured.slice(found.index, closeEnd), calls: [], suffix: suffixPart };
      }
    }
  }
  return { ready: false, prefix: '', calls: [], suffix: '' };
}

function findMatchingXMLToolWrapperClose(s, openTag, closeTag, openIdx) {
  const text = typeof s === 'string' ? s : '';
  const openTarget = String(openTag || '').toLowerCase();
  const closeTarget = String(closeTag || '').toLowerCase();
  if (!text || !openTarget || !closeTarget || openIdx < 0) {
    return -1;
  }
  const lower = text.toLowerCase();
  let depth = 1;
  for (let i = openIdx + openTarget.length; i < text.length;) {
    if (lower.startsWith('<![cdata[', i)) {
      const end = lower.indexOf(']]>', i + '<![cdata['.length);
      if (end < 0) {
        return -1;
      }
      i = end + ']]>'.length;
      continue;
    }
    if (lower.startsWith('<!--', i)) {
      const end = lower.indexOf('-->', i + '<!--'.length);
      if (end < 0) {
        return -1;
      }
      i = end + '-->'.length;
      continue;
    }
    if (lower.startsWith(closeTarget, i)) {
      depth -= 1;
      if (depth === 0) {
        return i;
      }
      i += closeTarget.length;
      continue;
    }
    if (lower.startsWith(openTarget, i) && hasXMLToolTagBoundary(text, i + openTarget.length)) {
      depth += 1;
      i += openTarget.length;
      continue;
    }
    i += 1;
  }
  return -1;
}

function findXMLOpenOutsideCDATA(s, openTag, start) {
  const text = typeof s === 'string' ? s : '';
  const target = String(openTag || '').toLowerCase();
  if (!text || !target) {
    return -1;
  }
  const lower = text.toLowerCase();
  for (let i = Math.max(0, start || 0); i < text.length;) {
    if (lower.startsWith('<![cdata[', i)) {
      const end = lower.indexOf(']]>', i + '<![cdata['.length);
      if (end < 0) {
        return -1;
      }
      i = end + ']]>'.length;
      continue;
    }
    if (lower.startsWith('<!--', i)) {
      const end = lower.indexOf('-->', i + '<!--'.length);
      if (end < 0) {
        return -1;
      }
      i = end + '-->'.length;
      continue;
    }
    if (lower.startsWith(target, i) && hasXMLToolTagBoundary(text, i + target.length)) {
      return i;
    }
    i += 1;
  }
  return -1;
}

function hasXMLToolTagBoundary(text, idx) {
  if (idx >= text.length) {
    return true;
  }
  return [' ', '\t', '\n', '\r', '>', '/'].includes(text[idx]);
}

function hasOpenXMLToolTag(captured) {
  const lower = captured.toLowerCase();
  for (const pair of XML_TOOL_TAG_PAIRS) {
    const openIdx = lower.indexOf(pair.open);
    if (openIdx >= 0) {
      if (findXMLCloseOutsideCDATA(captured, pair.close, openIdx + pair.open.length) < 0) {
        return true;
      }
    }
  }
  return false;
}

function containsAnyToolCallWrapper(lower) {
  return lower.includes('<tool_calls') ||
    lower.includes('<|dsml|tool_calls') ||
    lower.includes('<dsml|tool_calls') ||
    lower.includes('<｜tool_calls') ||
    lower.includes('<|tool_calls');
}

function firstInvokeIndex(lower) {
  const xmlIdx = lower.indexOf('<invoke');
  // Check all DSML-like invoke prefixes.
  const dsmlPrefixes = ['<|dsml|invoke', '<dsml|invoke', '<｜invoke', '<|invoke'];
  let dsmlIdx = -1;
  for (const prefix of dsmlPrefixes) {
    const idx = lower.indexOf(prefix);
    if (idx >= 0 && (dsmlIdx < 0 || idx < dsmlIdx)) {
      dsmlIdx = idx;
    }
  }
  if (xmlIdx < 0) {
    return { index: dsmlIdx, dsml: dsmlIdx >= 0 };
  }
  if (dsmlIdx < 0) {
    return { index: xmlIdx, dsml: false };
  }
  if (dsmlIdx < xmlIdx) {
    return { index: dsmlIdx, dsml: true };
  }
  return { index: xmlIdx, dsml: false };
}

function findPartialXMLToolTagStart(s) {
  const lastLT = s.lastIndexOf('<');
  if (lastLT < 0) {
    return -1;
  }
  const tail = s.slice(lastLT);
  if (tail.includes('>')) {
    return -1;
  }
  const lowerTail = tail.toLowerCase();
  for (const tag of XML_TOOL_OPENING_TAGS) {
    const tagWithLT = tag.startsWith('<') ? tag : '<' + tag;
    if (tagWithLT.startsWith(lowerTail)) {
      return lastLT;
    }
  }
  return -1;
}

function findXMLCloseOutsideCDATA(s, closeTag, start) {
  const text = typeof s === 'string' ? s : '';
  const target = String(closeTag || '').toLowerCase();
  if (!text || !target) {
    return -1;
  }
  const lower = text.toLowerCase();
  for (let i = Math.max(0, start || 0); i < text.length;) {
    if (lower.startsWith('<![cdata[', i)) {
      const end = lower.indexOf(']]>', i + '<![cdata['.length);
      if (end < 0) {
        return -1;
      }
      i = end + ']]>'.length;
      continue;
    }
    if (lower.startsWith('<!--', i)) {
      const end = lower.indexOf('-->', i + '<!--'.length);
      if (end < 0) {
        return -1;
      }
      i = end + '-->'.length;
      continue;
    }
    if (lower.startsWith(target, i)) {
      return i;
    }
    i += 1;
  }
  return -1;
}

module.exports = {
  consumeXMLToolCapture,
  hasOpenXMLToolTag,
  findPartialXMLToolTagStart,
};
