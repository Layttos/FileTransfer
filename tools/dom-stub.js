/*
 * DOM minimal, juste assez pour exécuter les scripts des pages hors navigateur.
 *
 * Le but n'est pas de simuler un navigateur, mais d'attraper les erreurs de
 * niveau supérieur : fonction appelée avant d'être définie, mauvais ordre de
 * chargement des scripts, faute de frappe dans un identifiant. Ce sont elles qui
 * tuent une page entière, et elles sont invisibles si l'on ne vérifie que le
 * code HTTP des réponses.
 */
'use strict';

function makeEl(tag) {
    return {
        tagName: String(tag || 'div').toUpperCase(),
        _attrs: {}, style: {}, dataset: {},
        children: [], value: '', textContent: '', innerHTML: '',
        disabled: false, files: [], lang: 'fr',
        setAttribute(k, v) { this._attrs[k] = String(v); },
        getAttribute(k) { return k in this._attrs ? this._attrs[k] : null; },
        removeAttribute(k) { delete this._attrs[k]; },
        addEventListener() {}, removeEventListener() {}, dispatchEvent() {},
        appendChild(c) { this.children.push(c); return c; },
        querySelector() { return makeEl('div'); },
        querySelectorAll() { return []; },
        closest() { return null; },
        focus() {}, select() {}, click() {}, remove() {},
        getBoundingClientRect() { return { width: 100, height: 100, top: 0, left: 0 }; },
        classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } }
    };
}

const documentStub = {
    documentElement: makeEl('html'),
    body: makeEl('body'),
    readyState: 'loading',
    title: '',
    cookie: '',
    createElement: makeEl,
    getElementById() { return makeEl('div'); },
    querySelector() { return makeEl('div'); },
    querySelectorAll() { return []; },
    addEventListener() {}, removeEventListener() {}, dispatchEvent() {}
};

// Dans un navigateur, window EST l'objet global : « window.t = t » rend donc t
// accessible tel quel. Sans cela le stub inventerait des erreurs inexistantes.
global.window = global;
global.document = documentStub;
global.matchMedia = () => ({ matches: false });
global.location = { href: '', hash: '', origin: 'http://localhost', search: '' };
global.navigator = { language: 'fr-FR', languages: ['fr-FR'], credentials: undefined, clipboard: { writeText: () => Promise.resolve() } };
global.localStorage = { getItem: () => null, setItem: () => {}, removeItem: () => {} };
global.PublicKeyCredential = undefined;
global.CustomEvent = function (n, o) { return { type: n, detail: o && o.detail }; };
global.File = function (parts, name, opts) { return { name, type: (opts || {}).type || '' }; };
global.FormData = function () { return { append() {} }; };
global.AbortController = function () { return { abort() {}, signal: {} }; };
global.XMLHttpRequest = function () { return { open() {}, send() {}, setRequestHeader() {}, upload: {} }; };
global.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}), text: () => Promise.resolve('') });
global.CodeMirror = undefined;
global.alert = () => {};
global.confirm = () => false;
global.prompt = () => null;
global.addEventListener = () => {};

module.exports = { makeEl, document: documentStub };
