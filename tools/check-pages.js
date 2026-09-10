/*
 * Exécute les scripts de chaque page du site et signale toute erreur.
 *
 * Le projet n'a aucune étape de build : rien ne relit le JavaScript des pages
 * avant qu'il n'arrive chez un visiteur. Ce script tient ce rôle. Il attrape les
 * erreurs qui tuent une page entière — fonction appelée avant d'être définie,
 * mauvais ordre de chargement, identifiant mal orthographié — et qui restent
 * invisibles si l'on ne vérifie que le code HTTP des réponses.
 *
 * Chaque page tourne dans son propre processus : un navigateur donne un contexte
 * global neuf à chaque page, et sans cette isolation les déclarations de la
 * première feraient échouer les suivantes.
 *
 * Usage :  node tools/check-pages.js
 */
'use strict';

const fs = require('fs');
const path = require('path');
const { execFileSync } = require('child_process');

const ROOT = path.resolve(__dirname, '..');
const PUBLIC = path.join(ROOT, 'public');

const PAGES = [
    'index.html', 'download.html', '404.html', 'api.html', 'api.en.html',
    'admin/login.html', 'admin/signin.html', 'admin/dashboard.html'
];

/* ---------- Mode « une page », exécuté dans un processus dédié ---------- */

if (process.argv[2]) {
    const vm = require('vm');
    require('./dom-stub.js');

    const html = fs.readFileSync(path.join(PUBLIC, process.argv[2]), 'utf8');
    const scripts = [];
    const re = /<script([^>]*)>([\s\S]*?)<\/script>/g;
    let m;

    while ((m = re.exec(html)) !== null) {
        const attrs = m[1];
        const body = m[2];
        const src = /src="([^"]+)"/.exec(attrs);
        const deferred = /\bdefer\b/.test(attrs);

        if (src) {
            // Seules les ressources locales sont chargées ; un CDN absent ne
            // change pas l'ordre que l'on cherche à vérifier.
            if (src[1].startsWith('/assets/')) {
                scripts.push({
                    name: src[1] + (deferred ? ' [defer]' : ''),
                    code: fs.readFileSync(path.join(PUBLIC, src[1]), 'utf8'),
                    defer: deferred
                });
            }
        } else if (body.trim()) {
            scripts.push({ name: 'inline', code: body, defer: false });
        }
    }

    // Un script « defer » s'exécute après le parsing, donc après les scripts
    // inline. Ne pas le modéliser masquerait un ordre de chargement fautif.
    scripts.sort((a, b) => (a.defer === b.defer ? 0 : a.defer ? 1 : -1));

    const context = vm.createContext(global);
    for (const script of scripts) {
        try {
            vm.runInContext(script.code, context, { filename: script.name });
        } catch (e) {
            process.stdout.write('\n#RESULT#ECHEC|' + script.name + '|' + e.message + '\n');
            process.exit(1);
        }
    }
    process.stdout.write('\n#RESULT#OK|' + scripts.length + '\n');
    process.exit(0);
}

/* ---------- Mode « toutes les pages » ---------- */

let failures = 0;

for (const page of PAGES) {
    if (!fs.existsSync(path.join(PUBLIC, page))) {
        console.log('  %s  ABSENTE', page.padEnd(24));
        failures++;
        continue;
    }

    let result;
    try {
        result = execFileSync(process.execPath, [__filename, page], {
            encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore']
        });
    } catch (e) {
        result = (e.stdout || '').toString() || '#RESULT#ECHEC|?|' + e.message;
    }

    // Une page peut écrire sur la sortie standard : on ne lit que notre marqueur.
    const line = String(result).split('\n').filter(function (l) {
        return l.indexOf('#RESULT#') === 0;
    }).pop() || '#RESULT#ECHEC|?|aucun résultat';
    const [status, where, message] = line.slice('#RESULT#'.length).split('|');
    if (status === 'OK') {
        console.log('  %s  ok (%s scripts)', page.padEnd(24), where);
    } else {
        console.log('  %s  ÉCHEC dans %s : %s', page.padEnd(24), where, message);
        failures++;
    }
}

if (failures) {
    console.error('\n  %d page(s) en échec.', failures);
    process.exit(1);
}
console.log('\n  Toutes les pages s\'exécutent sans erreur.');
