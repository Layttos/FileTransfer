/* Dictionnaire des pages publiques : accueil, telechargement, page 404. */
window.FT_DICT = Object.assign(window.FT_DICT || {}, {
    fr: Object.assign((window.FT_DICT || {}).fr || {}, {
        /* Commun */
        'theme.toLight': 'Passer au thème clair',
        'theme.toDark': 'Passer au thème sombre',
        'footer.version': 'FTransfer - Version 2.0',
        'footer.api': 'API',

        /* Accueil */
        'up.title': 'Upload de fichier',
        'up.brand': 'TeanoLink | Layttos',
        'up.heading': 'Téléverser des fichiers',
        'up.subtitle': 'Recherchez vos fichiers, puis cliquez sur « Téléverser les fichiers ».',
        'up.passwordHint': 'Laissez vide si vous ne souhaitez pas de protection',
        'up.passwordLabel': 'Mot de passe du fichier',
        'up.passwordPlaceholder': 'Protégez vos fichiers avec un mot de passe',
        'up.choose': 'Choisir des fichiers',
        'up.submit': 'Téléverser les fichiers',
        'up.progress': 'Téléversement : {percent} %',
        'up.progressStart': 'Téléversement : 0 %',
        'up.done': 'Téléversement terminé avec succès !',
        'up.doneErrors': 'Terminé avec des erreurs',
        'up.inProgress': 'En cours…',
        'up.failed': 'Échec',
        'up.copy': 'Copier',
        'up.copied': 'Copié !',
        'up.noFiles': 'Veuillez sélectionner au moins un fichier.',
        'up.pasted': '{n} fichier(s) collé(s), prêt(s) à être envoyé(s).',
        'up.pastedName': 'collage-{stamp}',
        'up.errTooLarge': 'Fichier trop volumineux : le serveur intermédiaire refuse la requête. Augmentez client_max_body_size sur le reverse proxy.',
        'up.errUnreachable': 'Serveur injoignable ({status})',
        'up.errTimeout': "Délai dépassé : le reverse proxy a coupé la connexion avant la fin de l'envoi.",
        'up.errAborted': "Connexion interrompue pendant l'envoi",
        'up.errNetwork': "Erreur réseau : connexion interrompue pendant l'envoi",
        'up.errUnreadable': 'Réponse illisible du serveur',
        'up.errGeneric': 'Erreur {status}',

        /* Téléchargement */
        'dl.title': 'Téléchargement de fichier',
        'dl.heading': 'Télécharger un fichier',
        'dl.subtitle': 'Cliquez sur « Télécharger le fichier » pour récupérer votre fichier.',
        'dl.fileName': 'Nom du fichier :',
        'dl.fileSize': 'Taille du fichier :',
        'dl.passwordLabel': 'Mot de passe du fichier',
        'dl.passwordPlaceholder': 'Mot de passe (optionnel)',
        'dl.submit': 'Télécharger le fichier',
        'dl.virustotal': 'Vérifier sur VirusTotal',
        'dl.error': 'Erreur de téléchargement',

        /* 404 */
        '404.title': '404 - Fichier introuvable',
        '404.heading': 'Fichier introuvable !',
        '404.body': "Le fichier demandé est introuvable. Vérifiez l'identifiant utilisé.",
        '404.contact': 'Si cela se trouve être une erreur, contactez Louison, ou Teano'
    }),

    en: Object.assign((window.FT_DICT || {}).en || {}, {
        /* Shared */
        'theme.toLight': 'Switch to light theme',
        'theme.toDark': 'Switch to dark theme',
        'footer.version': 'FTransfer - Version 2.0',
        'footer.api': 'API',

        /* Home */
        'up.title': 'File upload',
        'up.brand': 'TeanoLink | Layttos',
        'up.heading': 'Upload files',
        'up.subtitle': 'Pick your files, then click “Upload files”.',
        'up.passwordHint': 'Leave empty if you do not want protection',
        'up.passwordLabel': 'File password',
        'up.passwordPlaceholder': 'Protect your files with a password',
        'up.choose': 'Choose files',
        'up.submit': 'Upload files',
        'up.progress': 'Uploading: {percent}%',
        'up.progressStart': 'Uploading: 0%',
        'up.done': 'Upload finished successfully!',
        'up.doneErrors': 'Finished with errors',
        'up.inProgress': 'In progress…',
        'up.failed': 'Failed',
        'up.copy': 'Copy',
        'up.copied': 'Copied!',
        'up.noFiles': 'Please select at least one file.',
        'up.pasted': '{n} file(s) pasted, ready to upload.',
        'up.pastedName': 'pasted-{stamp}',
        'up.errTooLarge': 'File too large: the reverse proxy refused the request. Raise client_max_body_size on it.',
        'up.errUnreachable': 'Server unreachable ({status})',
        'up.errTimeout': 'Timed out: the reverse proxy closed the connection before the upload finished.',
        'up.errAborted': 'Connection interrupted during upload',
        'up.errNetwork': 'Network error: connection interrupted during upload',
        'up.errUnreadable': 'Unreadable response from the server',
        'up.errGeneric': 'Error {status}',

        /* Download */
        'dl.title': 'File download',
        'dl.heading': 'Download a file',
        'dl.subtitle': 'Click “Download file” to get your file.',
        'dl.fileName': 'File name:',
        'dl.fileSize': 'File size:',
        'dl.passwordLabel': 'File password',
        'dl.passwordPlaceholder': 'Password (optional)',
        'dl.submit': 'Download file',
        'dl.virustotal': 'Check on VirusTotal',
        'dl.error': 'Download error',

        /* 404 */
        '404.title': '404 - File not found',
        '404.heading': 'File not found!',
        '404.body': 'The requested file could not be found. Check the identifier you used.',
        '404.contact': 'If this turns out to be a mistake, contact Louison or Teano'
    })
});
