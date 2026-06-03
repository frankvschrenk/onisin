// upload-schaeden-to-s3.ts
//
// Reads all rows from public.pipeline_documents and uploads each one
// as a plain-text file to RustFS under s3://schaeden/<fall_nr>.txt.
//
// Run with:
//   bun run scripts/upload-schaeden-to-s3.ts

import postgres  from "postgres";
import { S3Client, PutObjectCommand, CreateBucketCommand, HeadBucketCommand } from "@aws-sdk/client-s3";

const ENDPOINT   = "http://localhost:9000";
const ACCESS_KEY = process.env.RUSTFS_ACCESS_KEY ?? "minioadmin";
const SECRET_KEY = process.env.RUSTFS_SECRET_KEY ?? "minioadmin123";
const BUCKET     = "schaeden";

const sql = postgres("postgresql://localhost/onisin", { max: 1 });

const s3 = new S3Client({
	endpoint:        ENDPOINT,
	region:          "us-east-1",
	credentials:     { accessKeyId: ACCESS_KEY, secretAccessKey: SECRET_KEY },
	forcePathStyle:  true,
});

// Ensure bucket exists.
try {
	await s3.send(new HeadBucketCommand({ Bucket: BUCKET }));
	console.log(`bucket "${BUCKET}" already exists`);
} catch {
	await s3.send(new CreateBucketCommand({ Bucket: BUCKET }));
	console.log(`bucket "${BUCKET}" created`);
}

// Load all documents.
const rows = await sql<Array<{
	fall_nr:   string;
	titel:     string;
	kategorie: string;
	inhalt:    string;
}>>`
	SELECT fall_nr, titel, kategorie, inhalt
	FROM   public.pipeline_documents
	ORDER  BY fall_nr
`;

console.log(`uploading ${rows.length} documents...`);

let ok = 0;
let fail = 0;

for (const row of rows) {
	const key     = `${row.fall_nr}.txt`;
	const content = [
		`Fall-Nr:   ${row.fall_nr}`,
		`Kategorie: ${row.kategorie}`,
		`Titel:     ${row.titel}`,
		``,
		row.inhalt,
	].join("\n");

	try {
		await s3.send(new PutObjectCommand({
			Bucket:      BUCKET,
			Key:         key,
			Body:        content,
			ContentType: "text/plain; charset=utf-8",
		}));
		console.log(`  ✓ ${key}`);
		ok++;
	} catch (err) {
		console.error(`  ✗ ${key}: ${err}`);
		fail++;
	}
}

await sql.end();
console.log(`\ndone: ${ok} uploaded, ${fail} failed`);
