/*
 * Vérifie les traductions :
 *   1. les dictionnaires français et anglais déclarent exactement les mêmes clés ;
 *   2. toute clé employée dans une page existe bien dans le dictionnaire qu'elle charge.
 *
 * Une clé manquante ne provoque pas d'erreur à l'exécution — le moteur retombe
 * sur le français, puis sur la clé brute — elle passerait donc inaperçue jusqu'à
 * ce qu'un visiteur anglophone tombe sur du français, ou sur « up.submit ».
 *
 * Usage :  node tools/check-i18n.js
 */
'use strict';

const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const PUBLIC = path.join(ROOT, 'public');

function load(file) {
    global.window = {};
    delete require.cache[require.resolve(path.join(PUBLIC, 'assets', file))];
    require(path.join(PUBLIC, 'assets', file));
    return JSON.parse(JSON.stringify(global.window.FT_DICT));
}

let problems = 0;

const DICTS = { 'lang-public.js': load('lang-public.js'), 'lang-admin.js': load('lang-admin.js') };

for (const [name, dict] of Object.entries(DICTS)) {
    const fr = new Set(Object.keys(dict.fr || {}));
    const en = new Set(Object.keys(dict.en || {}));
    const onlyFr = [...fr].filter((k) => !en.has(k));
    const onlyEn = [...en].filter((k) => !fr.has(k));

    if (onlyFr.length || onlyEn.length) {
        console.log('  %s  DÉSALIGNÉ', name.padEnd(18));
        if (onlyFr.length) console.log('      seulement en français : %s', onlyFr.join(', '));
        if (onlyEn.length) console.log('      seulement en anglais  : %s', onlyEn.join(', '));
        problems++;
    } else {
        console.log('  %s  %d clés, fr et en alignés', name.padEnd(18), fr.size);
    }
}

const PAGES = {
    'index.html': 'lang-public.js', 'download.html': 'lang-public.js', '404.html': 'lang-public.js',
    'admin/login.html': 'lang-admin.js', 'admin/signin.html': 'lang-admin.js',
    'admin/dashboard.html': 'lang-admin.js'
};

for (const [page, dictName] of Object.entries(PAGES)) {
    const html = fs.readFileSync(path.join(PUBLIC, page), 'utf8');
    const available = new Set(Object.keys(DICTS[dictName].fr));

    const used = new Set();
    for (const m of html.matchAll(/data-i18n(?:-[a-z]+)?="([^"]+)"/g)) used.add(m[1]);
    for (const m of html.matchAll(/\bt\('([a-zA-Z0-9_.\-]+)'/g)) used.add(m[1]);

    // Les clés composées à l'exécution, comme t('nav.' + id), sont hors de portée
    // d'une analyse statique : on écarte les préfixes nus.
    const missing = [...used].filter((k) => !available.has(k) && !k.endsWith('.'));

    if (missing.length) {
        console.log('  %s  clés absentes : %s', page.padEnd(24), missing.join(', '));
        problems++;
    } else {
        console.log('  %s  %d clés, toutes présentes', page.padEnd(24), used.size);
    }
}

if (problems) {
    console.error('\n  %d problème(s) de traduction.', problems);
    process.exit(1);
}
console.log('\n  Traductions cohérentes.');
