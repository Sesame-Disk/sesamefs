-- Backfill libraries_by_id table with existing library data

-- Library 1: unencrypted, no owner
INSERT INTO sesamefs.libraries_by_id (library_id, org_id, head_commit_id, encrypted)
VALUES (68c52780-8222-463a-bcad-9fa6db881f5c, 00000000-0000-0000-0000-000000000002, '0000000000000000000000000000000000000001', false);

-- Library 2: unencrypted
INSERT INTO sesamefs.libraries_by_id (library_id, org_id, owner_id, head_commit_id, encrypted)
VALUES (01920c46-b74b-4802-ad7c-db66732423ab, 00000000-0000-0000-0000-000000000001, 00000000-0000-0000-0000-000000000001, 'dfa028e23d05eb0878d577ea1d7065b458147cda', false);

-- Library 3: encrypted
INSERT INTO sesamefs.libraries_by_id (library_id, org_id, owner_id, head_commit_id, encrypted, enc_version, magic, random_key, salt, magic_strong, random_key_strong)
VALUES (23dcbc6c-8645-4843-92d9-a5900c4d2b3b, 00000000-0000-0000-0000-000000000001, 00000000-0000-0000-0000-000000000001, 'cdc5de7f7f06176bbbb57b25d63e147bf9cc6f89', true, 12, 'c2ea03d431714649c873aedbfc0bab94e20fe57576f8bee128204a5a06428a85', '7d0b470aa3a452ebbbf44d9a198a41c505245f875d4111a8cd827815a250ee08518d5ca4d12be57fa192c89032b43146', '4ce2a072198e6403883e9baab7e8cece7c94cab3921b1543e004b73b8356d597', '23c154bead80020418d3365ad3c9331fca4d3cce256de7807f39461d6378a35b', '47b2d06d5ef9f797473e71c679eed77bc33f3680438f5ba0b33f3092f9e44fe6a2b7cc84710716e8d95464d3dd6266f7');

-- Library 4: encrypted
INSERT INTO sesamefs.libraries_by_id (library_id, org_id, owner_id, head_commit_id, encrypted, enc_version, magic, random_key, salt, magic_strong, random_key_strong)
VALUES (54df4640-94ed-4b70-8edd-b9e5aed041da, 00000000-0000-0000-0000-000000000001, 00000000-0000-0000-0000-000000000001, '03d45ea3f468ce05d0468a432a255809e5aeba26', true, 12, 'c155fcf1338b9413a70cde52fff97368971d33f7178a82218cff4827b3a89e5f', '5e4e0bc3555b95aed22dc4131100c924ad765d2106f25ee1be44c2d300d000adfe896d36c13d738b1e4e86f3327dc973', '8b6028990bc0502f9f6c8fae8c9cd3d94e12370f41d11710abe478ed87fbe1c2', 'c1fc5d98f987257ad6635f68d8789581d334bbb51fcc92e38c9629f988707039', '9400190d84b9449834e1906759d4ae5d4a6ab23f8a40704f2212215c966189f6838fba68387888f190eeee5cb40dea72');
