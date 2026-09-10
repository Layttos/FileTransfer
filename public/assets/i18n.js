/*
 * Traduction du site, cote navigateur.
 *
 * La langue est resolue dans cet ordre : choix explicite conserve dans le
 * navigateur, puis langue du navigateur lui-meme. Tout ce qui n'est pas du
 * francais bascule en anglais.
 *
 * Le balisage porte des cles : data-i18n pour le texte, et les variantes
 * -placeholder, -title, -aria et -html pour les attributs et le contenu riche.
 * Les chaines construites en JavaScript passent par t().
 */
(function () {
    'use strict';

    var STORAGE_KEY = 'ft-lang';

    // Lu a chaque appel : l'ordre de chargement des scripts n'a pas d'importance,
    // et une page peut completer le dictionnaire apres coup.
    function dict() { return window.FT_DICT || {}; }

    function detect() {
        try {
            var saved = localStorage.getItem(STORAGE_KEY);
            if (saved === 'fr' || saved === 'en') return saved;
        } catch (e) { /* stockage indisponible : on suit le navigateur */ }

        var nav = (navigator.languages && navigator.languages[0]) || navigator.language || 'en';
        return String(nav).toLowerCase().indexOf('fr') === 0 ? 'fr' : 'en';
    }

    var lang = detect();

    // t renvoie la traduction d'une cle. Les variables {nom} y sont substituees.
    // Une cle absente retombe sur le francais, puis sur la cle elle-meme : une
    // traduction oubliee reste lisible au lieu de laisser un trou.
    function t(key, vars) {
        var D = dict();
        var table = D[lang] || D.en || {};
        var value = table[key];
        if (value === undefined && D.fr) value = D.fr[key];
        if (value === undefined) value = key;
        if (vars) {
            for (var name in vars) {
                if (Object.prototype.hasOwnProperty.call(vars, name)) {
                    value = value.split('{' + name + '}').join(vars[name]);
                }
            }
        }
        return value;
    }

    function apply(root) {
        var scope = root || document;

        scope.querySelectorAll('[data-i18n]').forEach(function (el) {
            el.textContent = t(el.getAttribute('data-i18n'));
        });
        scope.querySelectorAll('[data-i18n-html]').forEach(function (el) {
            el.innerHTML = t(el.getAttribute('data-i18n-html'));
        });
        ['placeholder', 'title', 'alt', 'value'].forEach(function (attr) {
            scope.querySelectorAll('[data-i18n-' + attr + ']').forEach(function (el) {
                el.setAttribute(attr, t(el.getAttribute('data-i18n-' + attr)));
            });
        });
        scope.querySelectorAll('[data-i18n-aria]').forEach(function (el) {
            el.setAttribute('aria-label', t(el.getAttribute('data-i18n-aria')));
        });

        document.documentElement.setAttribute('lang', lang);

        var title = document.querySelector('title[data-i18n]');
        if (title) document.title = t(title.getAttribute('data-i18n'));

        document.querySelectorAll('[data-lang-btn]').forEach(function (el) {
            var target = el.getAttribute('data-lang-btn');
            el.classList.toggle('lang-active', target === lang);
            el.setAttribute('aria-pressed', target === lang ? 'true' : 'false');
        });
    }

    function set(next) {
        if (next !== 'fr' && next !== 'en') return;
        lang = next;
        try { localStorage.setItem(STORAGE_KEY, next); } catch (e) { /* sans persistance */ }
        apply();
        // Les pages qui construisent du contenu en JavaScript se redessinent ici.
        document.dispatchEvent(new CustomEvent('ft:lang', { detail: next }));
    }

    // Bouton de bascule, insere par la page a l'endroit voulu.
    function switcher() {
        return '<div class="lang-switch" role="group" aria-label="Language">' +
            '<button type="button" data-lang-btn="fr">FR</button>' +
            '<button type="button" data-lang-btn="en">EN</button></div>';
    }

    document.addEventListener('click', function (ev) {
        var btn = ev.target.closest('[data-lang-btn]');
        if (btn) set(btn.getAttribute('data-lang-btn'));
    });

    window.t = t;
    window.ftLang = function () { return lang; };
    window.ftSetLang = set;
    window.ftApplyI18n = apply;
    window.ftLangSwitcher = switcher;

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', function () { apply(); });
    } else {
        apply();
    }
})();
