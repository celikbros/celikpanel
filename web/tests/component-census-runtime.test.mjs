import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test from 'node:test';
import { pathToFileURL } from 'node:url';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const reactURL = pathToFileURL(require.resolve('react')).href;

// The sidebar badge is a count, and a count can be wrong in three different
// ways: it can report an inventory nobody took, it can lag the screen beside
// it, and it can disagree with it. The store these tests exercise is what the
// badge reads, so the arithmetic and the notification are pinned here rather
// than inferred from the shell that renders them.
//
// Kenar çubuğu rozeti bir sayıdır ve bir sayı üç ayrı biçimde yanlış olabilir:
// kimsenin yapmadığı bir envanteri bildirebilir, yanındaki ekranın gerisinde
// kalabilir ve onunla çelişebilir.

async function importTypeScript(relativePath) {
    const source = readFileSync(new URL(relativePath, import.meta.url), 'utf8');
    const compiled = ts.transpileModule(source, {
        compilerOptions: {
            module: ts.ModuleKind.ES2022,
            target: ts.ScriptTarget.ES2020,
        },
    }).outputText
        // A data: module cannot resolve a bare specifier, so the one import
        // this store has is pointed at the real file.
        // data: modulu ciplak bir belirteci cozemez.
        .replace(/from ['"]react['"]/g, `from '${reactURL}'`);
    return import('data:text/javascript;base64,' + Buffer.from(compiled).toString('base64'));
}

const census = await importTypeScript('../src/lib/componentCensus.ts');

const {
    componentCensusFrom,
    publishComponentCensus,
    readComponentCensus,
    subscribeComponentCensus,
    resetComponentCensus,
} = census;

test('a host nobody has observed has no number, and zero is not the answer', () => {
    // Every row unobserved: there is no census, so there is nothing to show.
    // A badge reading 0 here would report an inventory nobody took (R-040).
    assert.equal(componentCensusFrom([{ is_installed: null }, { is_installed: null }]), null);
    // An empty catalogue is a different thing: nothing to count is zero.
    assert.equal(componentCensusFrom([]), 0);
    // Observed and absent really is zero.
    assert.equal(componentCensusFrom([{ is_installed: false }, { is_installed: false }]), 0);
});

test('a partly observed host counts what is known and nothing else', () => {
    // The unchecked row joins neither side of the count.
    // Bakılmamış satır sayımın hiçbir yakasına katılmaz.
    assert.equal(
        componentCensusFrom([
            { is_installed: true },
            { is_installed: false },
            { is_installed: null },
        ]),
        1,
    );
});

test('a value this panel cannot read is not an observation it has made', () => {
    // Fail closed: an undecodable field is unobserved, never "installed".
    // Kapalı düş: çözülemeyen alan gözlenmemiştir, asla "kurulu" değildir.
    assert.equal(componentCensusFrom([{ is_installed: 'yes' }, { is_installed: 1 }]), null);
    assert.equal(componentCensusFrom([{}]), null);
    assert.equal(componentCensusFrom([{ is_installed: true }, { is_installed: 'yes' }]), 1);
});

test('the badge follows a check run on any screen, without asking again', () => {
    resetComponentCensus();
    // Nothing published yet is not the same as "no census": the shell simply
    // has no answer to draw.
    assert.equal(readComponentCensus(), undefined);

    const seen = [];
    const unsubscribe = subscribeComponentCensus(() => seen.push(readComponentCensus()));

    // The shell's own read lands first: a never-checked host.
    publishComponentCensus([{ is_installed: null }, { is_installed: null }]);
    assert.equal(readComponentCensus(), null);

    // Now the operator runs a check somewhere else entirely - the dashboard,
    // or a component page. That screen publishes what it found, and the badge
    // has the new number without a reload and without a second request.
    // Operatör kontrolü bambaşka bir yerde çalıştırır; o ekran bulduğunu
    // yayınlar ve rozet yeni sayıya yeniden yükleme olmadan sahip olur.
    publishComponentCensus([{ is_installed: true }, { is_installed: false }]);
    assert.equal(readComponentCensus(), 1);
    assert.deepEqual(seen, [null, 1]);

    // An identical answer is not a re-render: the shell is not woken for a
    // number that did not change.
    publishComponentCensus([{ is_installed: true }, { is_installed: false }]);
    assert.deepEqual(seen, [null, 1]);

    unsubscribe();
    publishComponentCensus([{ is_installed: null }]);
    assert.deepEqual(seen, [null, 1], 'an unsubscribed shell must not be notified');
    assert.equal(readComponentCensus(), null);
    resetComponentCensus();
});
