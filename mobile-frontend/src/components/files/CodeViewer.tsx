import React, { useState, useEffect, useRef } from 'react';
import { X, Download, Copy } from 'lucide-react';
import { downloadFile } from '../../lib/share';
import { getFileExtension } from '../../lib/utils';
import hljs from 'highlight.js/lib/core';

// Register common languages
import javascript from 'highlight.js/lib/languages/javascript';
import typescript from 'highlight.js/lib/languages/typescript';
import python from 'highlight.js/lib/languages/python';
import go from 'highlight.js/lib/languages/go';
import java from 'highlight.js/lib/languages/java';
import c from 'highlight.js/lib/languages/c';
import cpp from 'highlight.js/lib/languages/cpp';
import rust from 'highlight.js/lib/languages/rust';
import bash from 'highlight.js/lib/languages/bash';
import ruby from 'highlight.js/lib/languages/ruby';
import php from 'highlight.js/lib/languages/php';
import sql from 'highlight.js/lib/languages/sql';
import xml from 'highlight.js/lib/languages/xml';
import css from 'highlight.js/lib/languages/css';
import json from 'highlight.js/lib/languages/json';
import yaml from 'highlight.js/lib/languages/yaml';
import lua from 'highlight.js/lib/languages/lua';
import swift from 'highlight.js/lib/languages/swift';
import kotlin from 'highlight.js/lib/languages/kotlin';
import scala from 'highlight.js/lib/languages/scala';
import dart from 'highlight.js/lib/languages/dart';

hljs.registerLanguage('javascript', javascript);
hljs.registerLanguage('typescript', typescript);
hljs.registerLanguage('python', python);
hljs.registerLanguage('go', go);
hljs.registerLanguage('java', java);
hljs.registerLanguage('c', c);
hljs.registerLanguage('cpp', cpp);
hljs.registerLanguage('rust', rust);
hljs.registerLanguage('bash', bash);
hljs.registerLanguage('ruby', ruby);
hljs.registerLanguage('php', php);
hljs.registerLanguage('sql', sql);
hljs.registerLanguage('xml', xml);
hljs.registerLanguage('css', css);
hljs.registerLanguage('json', json);
hljs.registerLanguage('yaml', yaml);
hljs.registerLanguage('lua', lua);
hljs.registerLanguage('swift', swift);
hljs.registerLanguage('kotlin', kotlin);
hljs.registerLanguage('scala', scala);
hljs.registerLanguage('dart', dart);

const EXT_TO_LANG: Record<string, string> = {
  js: 'javascript', jsx: 'javascript', ts: 'typescript', tsx: 'typescript',
  py: 'python', go: 'go', java: 'java', c: 'c', cpp: 'cpp', h: 'c', hpp: 'cpp',
  rs: 'rust', sh: 'bash', bash: 'bash', zsh: 'bash', rb: 'ruby', php: 'php',
  sql: 'sql', html: 'xml', xml: 'xml', css: 'css', scss: 'css', less: 'css',
  vue: 'xml', svelte: 'xml', json: 'json', yml: 'yaml', yaml: 'yaml',
  lua: 'lua', swift: 'swift', kt: 'kotlin', scala: 'scala', dart: 'dart',
};

interface CodeViewerProps {
  url: string;
  fileName: string;
  onClose: () => void;
  onToast?: (msg: string) => void;
}

export default function CodeViewer({ url, fileName, onClose, onToast }: CodeViewerProps) {
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const codeRef = useRef<HTMLElement>(null);

  const ext = getFileExtension(fileName);
  const lang = EXT_TO_LANG[ext] || '';

  useEffect(() => {
    fetch(url)
      .then(res => {
        if (!res.ok) throw new Error('Failed to load file');
        return res.text();
      })
      .then(text => {
        setContent(text);
        setLoading(false);
      })
      .catch(err => {
        setError(err.message);
        setLoading(false);
      });
  }, [url]);

  useEffect(() => {
    if (codeRef.current && content && lang) {
      try {
        const result = hljs.highlight(content, { language: lang });
        codeRef.current.innerHTML = result.value;
      } catch {
        // If highlighting fails, show plain text
        codeRef.current.textContent = content;
      }
    }
  }, [content, lang]);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(content);
      onToast?.('Copied to clipboard');
    } catch {
      onToast?.('Failed to copy');
    }
  };

  const lines = content.split('\n');

  return (
    <div className="fixed inset-0 z-50 bg-white flex flex-col" data-testid="code-viewer">
      {/* Top bar */}
      <div className="flex items-center justify-between p-2 border-b border-gray-200">
        <button
          onClick={onClose}
          className="min-h-[44px] min-w-[44px] flex items-center justify-center text-gray-600"
          aria-label="Close"
        >
          <X className="w-6 h-6" />
        </button>
        <p className="text-text text-sm truncate mx-2 flex-1 text-center font-medium">{fileName}</p>
        <div className="flex gap-1">
          <button
            onClick={handleCopy}
            className="min-h-[44px] min-w-[44px] flex items-center justify-center text-gray-600"
            aria-label="Copy"
          >
            <Copy className="w-5 h-5" />
          </button>
          <button
            onClick={() => downloadFile(url, fileName)}
            className="min-h-[44px] min-w-[44px] flex items-center justify-center text-gray-600"
            aria-label="Download"
          >
            <Download className="w-5 h-5" />
          </button>
        </div>
      </div>

      {/* Code */}
      <div className="flex-1 overflow-auto">
        {loading && <p className="text-center text-gray-500 py-8">Loading...</p>}
        {error && <p className="text-center text-red-500 py-4">{error}</p>}
        {!loading && !error && (
          <div className="flex text-sm font-mono">
            {/* Line numbers */}
            <div className="flex-shrink-0 text-right text-gray-400 select-none bg-gray-50 py-4 px-2 border-r border-gray-200">
              {lines.map((_, i) => (
                <div key={i} className="leading-5 px-1">{i + 1}</div>
              ))}
            </div>
            {/* Code content */}
            <pre className="flex-1 py-4 px-4 overflow-x-auto">
              <code ref={codeRef} className={lang ? `hljs language-${lang}` : ''}>
                {content}
              </code>
            </pre>
          </div>
        )}
      </div>
    </div>
  );
}
