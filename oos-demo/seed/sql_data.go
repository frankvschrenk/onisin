package seed

// sql_data.go — reference tables, persons and notes for the OOS demo.

import (
	"database/sql"
)

// seedRefTables populates all reference tables (idempotent via TRUNCATE + INSERT).
func seedRefTables(db *sql.DB) error {
	_, err := db.Exec(`
		TRUNCATE public.note, public.person RESTART IDENTITY CASCADE;
		TRUNCATE public.city                RESTART IDENTITY CASCADE;
		TRUNCATE public.language            RESTART IDENTITY CASCADE;
		TRUNCATE public.notify_channel      RESTART IDENTITY CASCADE;
		TRUNCATE public.employment_type     RESTART IDENTITY CASCADE;
		TRUNCATE public.department          RESTART IDENTITY CASCADE;
		TRUNCATE public.role                RESTART IDENTITY CASCADE;
		TRUNCATE public.country             RESTART IDENTITY CASCADE;

		INSERT INTO public.country (code, name) VALUES
			('de','Deutschland'),('at','Österreich'),('ch','Schweiz'),
			('gb','Vereinigtes Königreich'),('us','Vereinigte Staaten'),
			('nl','Niederlande'),('fr','Frankreich'),('it','Italien'),
			('es','Spanien'),('pl','Polen'),('se','Schweden'),
			('no','Norwegen'),('dk','Dänemark'),('cn','China'),
			('jp','Japan'),('au','Australien'),
			('ae','Vereinigte Arabische Emirate'),('mx','Mexiko');

		INSERT INTO public.city (name, country_code) VALUES
			('München','de'),('Berlin','de'),('Hamburg','de'),
			('Frankfurt','de'),('Köln','de'),('Stuttgart','de'),
			('Düsseldorf','de'),('Leipzig','de'),('Nürnberg','de'),
			('Wien','at'),('Graz','at'),('Salzburg','at'),
			('Zürich','ch'),('Genf','ch'),('Basel','ch'),
			('London','gb'),('Manchester','gb'),('Birmingham','gb'),
			('Amsterdam','nl'),('Rotterdam','nl'),
			('Paris','fr'),('Lyon','fr'),
			('Rom','it'),('Mailand','it'),
			('Madrid','es'),('Barcelona','es'),
			('Warschau','pl'),('Krakau','pl'),
			('Stockholm','se'),('Göteborg','se'),
			('Tokyo','jp'),('Shanghai','cn'),('Melbourne','au'),
			('Dubai','ae'),('Mexiko-Stadt','mx');

		INSERT INTO public.role (key, label) VALUES
			('admin','Administrator'),('manager','Manager'),
			('user','Benutzer'),('guest','Gast');

		INSERT INTO public.department (key, label) VALUES
			('architecture','Enterprise Architektur'),
			('development','Softwareentwicklung'),
			('operations','Betrieb & Infrastruktur'),
			('sales','Vertrieb'),('hr','Personal'),
			('finance','Finanzen'),('legal','Recht & Compliance');

		INSERT INTO public.employment_type (key, label) VALUES
			('fulltime','Vollzeit'),('parttime','Teilzeit'),
			('freelance','Freiberuflich'),('intern','Praktikum');

		INSERT INTO public.notify_channel (key, label) VALUES
			('push','Push'),('email','E-Mail'),('sms','SMS');

		INSERT INTO public.language (code, name) VALUES
			('de','Deutsch'),('en','English'),('fr','Français'),
			('it','Italiano'),('es','Español'),('nl','Nederlands'),
			('pl','Polski'),('sv','Svenska'),('zh','中文'),('ja','日本語');
	`)
	return err
}

// seedPersons inserts the demo persons.
func seedPersons(db *sql.DB) error {
	_, err := db.Exec(`
		INSERT INTO public.person (
			title, firstname, lastname, age, net_worth,
			role, department, employment, active, profile_complete,
			street, zip, city, country,
			email, phone, mobile, linkedin,
			notify_channel, notify_email, notify_push, notify_sms, notify_weekly,
			language, font_size, notes, source
		) VALUES
		('Dr.','Elena','Kovač',38,0,
		 'admin','architecture','fulltime',true,0.82,
		 'Leopoldstraße 18','80802','München','de',
		 'elena.kovac@oos.local','+49 89 4512 0','+49 170 9823 441','linkedin.com/in/elena-kovac',
		 'push',true,true,false,true,'de',14,
		 'Ansprechpartnerin für alle Cloud-Projekte im DACH-Raum.','OOS Demo'),

		(NULL,'James','Thompson',50,4806.00,
		 'manager','sales','fulltime',true,0.65,
		 '47 Baker Street','W1U 7BJ','London','gb',
		 'james.thompson@oos.local','+44 20 79460000','+44 7700 900123','linkedin.com/in/james-thompson',
		 'email',true,false,false,true,'en',14,
		 'Dokumente angefordert. Gehaltsnachweis fehlt.','OOS Demo'),

		(NULL,'Yuki','Tanaka',26,0.00,
		 'user','development','intern',true,0.40,
		 'Shibuya 3-12-5','150-0002','Tokyo','jp',
		 'yuki.tanaka@oos.local','+81 3 12345678','+81 90 1234 5678',NULL,
		 'push',true,true,true,false,'en',12,NULL,'OOS Demo'),

		(NULL,'Michael','O''Brien',60,1251.00,
		 'manager','operations','parttime',true,0.90,
		 '88 Collins Street','3000','Melbourne','au',
		 'michael.obrien@oos.local','+61 3 98765432','+61 412 345 678','linkedin.com/in/mobrien',
		 'email',true,false,false,false,'en',16,
		 'Kunde seit 2015. Bevorzugte Kommunikation per E-Mail.','OOS Demo'),

		('Dr.','Amira','Hassan',46,320000.75,
		 'manager','architecture','fulltime',true,0.75,
		 'Al Wasl Road 22','00000','Frankfurt','de',
		 'amira.hassan@oos.local','+971 4 3456789','+971 50 123 4567','linkedin.com/in/amira-hassan',
		 'push',true,true,false,true,'de',14,NULL,'OOS Demo'),

		(NULL,'Lars','Eriksson',74,2100000.00,
		 'guest','sales','freelance',false,0.30,
		 'Kungsgatan 9','11143','Berlin','de',
		 'lars.eriksson@oos.local','+46 8 123456',NULL,NULL,
		 'email',true,false,false,false,'de',14,
		 'Risikohinweis erteilt. Auf konservative Anlageformen hingewiesen.','OOS Demo'),

		(NULL,'Valentina','Rossi',31,54000.00,
		 'user','development','fulltime',true,0.55,
		 'Via Condotti 3','00187','Hamburg','de',
		 'valentina.rossi@oos.local','+39 06 12345678','+39 347 123 4567','linkedin.com/in/valentina-rossi',
		 'push',false,true,true,false,'de',13,
		 'Budgetberatung: Monatliche Sparrate von 300 € vereinbart.','OOS Demo'),

		(NULL,'Daniel','Kowalski',48,780000.00,
		 'admin','operations','fulltime',true,0.88,
		 'ul. Nowy Świat 6','00-400','München','de',
		 'daniel.kowalski@oos.local','+48 22 8765432','+48 601 234 567','linkedin.com/in/dkowalski',
		 'sms',true,true,true,true,'de',14,
		 'Immobilienfinanzierung angefragt. Rückruf ausstehend.','OOS Demo'),

		('Prof.','Lin','Wei',45,1650000.00,
		 'manager','architecture','fulltime',true,0.95,
		 'Nanjing Road 18','200001','Berlin','de',
		 'lin.wei@oos.local','+86 21 63219876','+86 138 0013 8000','linkedin.com/in/lin-wei',
		 'push',true,true,false,true,'en',15,
		 'Jahresgespräch: Sehr zufrieden. Empfehlung zugesagt.','OOS Demo'),

		(NULL,'Carlos','Mendoza',22,12000.00,
		 'user','development','intern',true,0.25,
		 'Av. Insurgentes Sur 5','06600','München','de',
		 'carlos.mendoza@oos.local','+52 55 12345678',NULL,NULL,
		 'push',true,true,false,false,'de',14,
		 'Student. Erstes Depot besprochen.','OOS Demo');
	`)
	return err
}

// seedNotes inserts the demo notes.
func seedNotes(db *sql.DB) error {
	_, err := db.Exec(`
		INSERT INTO public.note (person_id, title, body) VALUES
		(1,'Cloud-Migration DACH',         'Kickoff-Meeting am 15.01. Infrastruktur-Analyse abgeschlossen.'),
		(1,'Architektur-Review Q1',        'Review-Termin steht. Elena übernimmt Moderation.'),
		(2,'Dokumente angefordert',        'Gehaltsnachweis und Kontoauszüge fehlen noch.'),
		(2,'Steuerberatung koordiniert',   'Kontakt zu Steuerberater Dr. Hassan hergestellt.'),
		(4,'VIP Status bestätigt',         'Kunde seit 2015. Bevorzugte Kommunikation per E-Mail.'),
		(4,'Nachfolgeplanung',             'Gespräch über Vermögensübertragung vereinbart.'),
		(6,'Risikohinweis erteilt',        'Aufgrund des Alters auf konservative Anlageformen hingewiesen.'),
		(7,'Budgetberatung',               'Monatliche Sparrate von 300 € vereinbart.'),
		(8,'Immobilienfinanzierung',       'Anfrage für Anschlussfinanzierung bis Q3 2025.'),
		(8,'Rückruf ausstehend',           'Keine Erreichbarkeit seit 14 Tagen.'),
		(9,'Jahresgespräch abgeschlossen', 'Sehr zufrieden. Empfehlung an Bekannte zugesagt.'),
		(10,'Onboarding',                  'Kontoeröffnung abgeschlossen.');
	`)
	return err
}
