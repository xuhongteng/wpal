/* 极简 i18n 运行时。
 *
 * 不引入任何前端框架/库：整个看板要塞进单个二进制，
 * 每多一个依赖就多几十 KB，而一个字典加载器只需要几十行。
 *
 * 用法：
 *   await I18N.init();            // 加载字典
 *   I18N.t('metric.cpu')          // 取文案
 *   I18N.t('verdict.warn', {n:2}) // 带参数
 *   I18N.onChange(renderAll);     // 语言切换后重渲染
 */
(() => {
  'use strict';

  const LANGS = {
    'zh-CN': { label: '中文', htmlLang: 'zh-CN' },
    'en-US': { label: 'English', htmlLang: 'en' },
  };
  const DEFAULT_LANG = 'zh-CN';
  const STORAGE_KEY = 'wpal.lang';

  const dicts = {};
  const listeners = [];
  let current = DEFAULT_LANG;

  function detect() {
    const url = new URLSearchParams(location.search).get('lang');
    if (url && LANGS[url]) return url;
    try {
      const saved = localStorage.getItem(STORAGE_KEY);
      if (saved && LANGS[saved]) return saved;
    } catch (e) { /* 隐私模式下 localStorage 不可用 */ }
    return DEFAULT_LANG;
  }

  function interpolate(tpl, params) {
    if (!params) return tpl;
    return tpl.replace(/\{(\w+)\}/g, (m, k) =>
      Object.prototype.hasOwnProperty.call(params, k) ? String(params[k]) : m);
  }

  const I18N = {
    get lang() { return current; },
    get languages() { return LANGS; },

    async load(lang) {
      if (dicts[lang]) return dicts[lang];
      const res = await fetch(`static/locales/${lang}.json`, { cache: 'no-cache' });
      if (!res.ok) throw new Error(`加载语言包失败: ${lang}`);
      dicts[lang] = await res.json();
      return dicts[lang];
    },

    async init() {
      current = detect();
      try {
        await this.load(current);
      } catch (e) {
        dicts[current] = dicts[current] || {};
      }
      document.documentElement.lang = LANGS[current].htmlLang;
      return current;
    },

    t(key, params) {
      const d = dicts[current] || {};
      let s = d[key];
      if (s === undefined) {
        // 回退到默认语言，再回退到 key 本身
        const fb = dicts[DEFAULT_LANG] || {};
        s = fb[key] !== undefined ? fb[key] : key;
      }
      return interpolate(s, params);
    },

    async setLang(lang) {
      if (!LANGS[lang] || lang === current) return;
      await this.load(lang);
      current = lang;
      try { localStorage.setItem(STORAGE_KEY, lang); } catch (e) { /* ignore */ }
      document.documentElement.lang = LANGS[lang].htmlLang;
      listeners.forEach((fn) => { try { fn(lang); } catch (e) { /* ignore */ } });
    },

    async toggle() {
      await this.setLang(current === DEFAULT_LANG ? 'en-US' : DEFAULT_LANG);
    },

    onChange(fn) { listeners.push(fn); },
  };

  window.I18N = I18N;
})();
